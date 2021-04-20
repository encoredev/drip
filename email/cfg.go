package email

import (
	_ "embed"
	"encoding/json"
	"log"
)

//go:embed config.json
var cfgData []byte

var cfg struct {
	StrapiURL     string `json:"strapi_url"`
	MailgunDomain string `json:"mailgun_domain"`
}

func init() {
	if err := json.Unmarshal(cfgData, &cfg); err != nil {
		log.Fatalln("could not decode config:", err)
	}
}

var secrets struct {
	MailGunAPIKey string
	AuthPassword  string
	TokenHashKey  string
}
