package email

import (
	"github.com/shurcooL/graphql"
)

type Template struct {
	ID       string
	Subject  string
	Sender   string
	HTMLBody string         `graphql:"html_body"`
	TextBody string         `graphql:"text_body"`
	Files    []TemplateFile `graphql:"images"`
}

type TemplateFile struct {
	Name   string
	URL    string
	Width  int
	Height int
}

var gql = graphql.NewClient(cfg.StrapiURL+"/graphql", nil)
