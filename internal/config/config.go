package config

import "leblanc.io/open-go-base/appconf"

// Config holds the application configuration loaded from a YAML file or
// environment variables (prefix OGM_, "Open Go Menu"). Every concern is
// described by a shared appconf fragment, plus the families' own; the prefix
// is applied here, at the composition point, since cleanenv only supports a
// static prefix.
type Config struct {
	Web      appconf.Web     `yaml:"web"      env-prefix:"OGM_"`
	Log      appconf.Logging `yaml:"logging"  env-prefix:"OGM_"`
	I18n     appconf.I18n    `yaml:"i18n"     env-prefix:"OGM_"`
	Auth     appconf.Auth    `yaml:"auth"     env-prefix:"OGM_"`
	Families Families        `yaml:"families" env-prefix:"OGM_"`
}

// Families configures the accounts: where the data lives, who may sign up and
// when an unused family is deleted.
type Families struct {
	DataDir        string `yaml:"data_dir"        env:"DATA_DIR"        env-default:"data"   env-description:"Directory of the accounts database and of one database per family"`
	Signup         string `yaml:"signup"          env:"SIGNUP"          env-default:"invite" env-description:"Who may create a family: open|invite|closed"`
	InactiveMonths int    `yaml:"inactive_months" env:"INACTIVE_MONTHS" env-default:"12"     env-description:"Months without any visit after which purge deletes a family"`
}

// Signup modes.
const (
	SignupOpen   = "open"
	SignupInvite = "invite"
	SignupClosed = "closed"
)
