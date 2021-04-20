package email

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"encore.dev/rlog"
	"encore.dev/storage/sqldb"
	"github.com/shurcooL/graphql"
	"golang.org/x/sync/errgroup"
)

type Stream struct {
	ID          string
	StreamSteps []*StreamStep `graphql:"stream_steps"`
}

type StreamStep struct {
	ID           string
	DelaySeconds int `graphql:"delay_seconds"`
	Template     *Template
}

type TriggerSeriesParams struct {
	Email      string
	SeriesName string
}

// BeginSeries begins the given email series for a given email.
//encore:api auth
func BeginSeries(ctx context.Context, params *TriggerSeriesParams) error {
	if err := ensureUserCreated(ctx, params.Email); err != nil {
		return err
	}
	ser, err := getSeries(ctx, params.SeriesName)
	if err != nil {
		return err
	} else if len(ser.StreamSteps) == 0 {
		return fmt.Errorf("missing initial step")
	}
	step := ser.StreamSteps[0]
	delay := time.Duration(step.DelaySeconds) * time.Second

	_, err = sqldb.Exec(ctx, `
		INSERT INTO "user_series" (email, series_id, next_step_id, next_step_ts, started)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (email, series_id) DO NOTHING
	`, params.Email, params.SeriesName, step.ID, time.Now().Add(delay))
	if err != nil {
		return fmt.Errorf("insert user_series: %v", err)
	}
	return err
}

// TriggerSeries triggers due emails.
//encore:api auth
func TriggerSeries(ctx context.Context) error {
	queue, err := queryDueEmails(ctx)
	if err != nil {
		return err
	} else if len(queue) == 0 {
		rlog.Info("no due emails to send")
		return nil
	}
	rlog.Info("sending due emails", "n", len(queue))

	// Get all series
	streams := make(map[string]*Stream)
	{
		g, streamCtx := errgroup.WithContext(ctx)
		for _, e := range queue {
			id := e.SeriesID
			if _, ok := streams[id]; !ok {
				streams[id] = nil // mark as started
				g.Go(func() error {
					s, err := getSeries(streamCtx, id)
					if err == nil {
						streams[id] = s
					}
					return err
				})
			}
		}
		if err := g.Wait(); err != nil {
			return err
		}
	}

	type result struct {
		e       *queuedEmail
		next    *StreamStep
		emailID int64
		err     error
	}
	res := make(chan result, len(queue))

	for _, e := range queue {
		e := e
		go func() {
			ser := streams[e.SeriesID]
			curr, next, ok := findStep(ser, e.StepID)
			if !ok {
				res <- result{e: e, err: fmt.Errorf("unknown step %s", e.StepID)}
				return
			}

			resp, err := Send(ctx, &SendParams{
				Template: curr.Template,
				Email:    e.Email,
			})
			if err != nil {
				res <- result{e: e, err: err}
			} else {
				res <- result{e: e, next: next, emailID: resp.ID}
			}
		}()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	for i := 0; i < len(queue); i++ {
		r := <-res
		if r.err != nil || r.emailID == 0 {
			continue
		}

		if err := updateUserSeriesStep(ctx, r.e, r.next, r.emailID); err != nil {
			rlog.Error("could not update user series step",
				"email", r.e.Email,
				"err", err)
		}
	}

	return nil
}

func updateUserSeriesStep(ctx context.Context, e *queuedEmail, next *StreamStep, emailID int64) error {
	_, err := sqldb.Exec(ctx, `
		INSERT INTO "user_series_step" (email, series_id, step_id, email_id, executed)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (email, series_id, step_id) DO UPDATE
		SET email_id = $4
	`, e.Email, e.SeriesID, e.StepID, emailID)
	if err != nil {
		return fmt.Errorf("insert user_series_step: %v", err)
	}

	var (
		nextTS sql.NullTime
		nextID *string
	)
	if next != nil {
		nextID = &next.ID
		nextTS.Time = time.Now().Add(time.Duration(next.DelaySeconds) * time.Second)
		nextTS.Valid = true
	}

	_, err = sqldb.Exec(ctx, `
		UPDATE "user_series"
		SET next_step_id = $3, next_step_ts = $4
		WHERE email = $1 AND series_id = $2
	`, e.Email, e.SeriesID, nextID, nextTS)
	if err != nil {
		return fmt.Errorf("update user_series: %v", err)
	}
	return nil
}

type queuedEmail struct {
	Email    string
	SeriesID string
	StepID   string
}

func queryDueEmails(ctx context.Context) ([]*queuedEmail, error) {
	rows, err := sqldb.Query(ctx, `
		SELECT us.email, series_id, next_step_id
		FROM "user_series" us
		INNER JOIN "user" u ON (u.email = us.email)
		WHERE next_step_ts <= NOW() AND next_step_id IS NOT NULL AND u.optin
		LIMIT 10
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var q []*queuedEmail

	for rows.Next() {
		var e queuedEmail
		err := rows.Scan(&e.Email, &e.SeriesID, &e.StepID)
		if err != nil {
			return nil, fmt.Errorf("scan rows: %v", err)
		}
		q = append(q, &e)
	}
	return q, rows.Err()
}

func getSeries(ctx context.Context, name string) (*Stream, error) {
	var query struct {
		Streams []*Stream `graphql:"streams(where: {name: $name})"`
	}
	err := gql.Query(ctx, &query, map[string]interface{}{
		"name": graphql.String(name),
	})
	if err != nil {
		return nil, err
	} else if len(query.Streams) == 0 {
		return nil, fmt.Errorf("unknown stream")
	}
	return query.Streams[0], nil
}

func findStep(stream *Stream, id string) (curr, next *StreamStep, ok bool) {
	for i, s := range stream.StreamSteps {
		if s.ID == id {
			if len(stream.StreamSteps) > (i + 1) {
				next = stream.StreamSteps[i+1]
			}
			return s, next, true
		}
	}
	return nil, nil, false
}
