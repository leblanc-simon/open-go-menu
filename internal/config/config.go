package config

import "leblanc.io/open-go-base/appconf"

// Config holds the application configuration loaded from a YAML file or
// environment variables (prefix OGM_, "Open Go Menu"). Every concern is
// described by a shared appconf fragment; the prefix is applied here, at the
// composition point, since cleanenv only supports a static prefix.
type Config struct {
	Web  appconf.Web      `yaml:"web"      env-prefix:"OGM_"`
	Log  appconf.Logging  `yaml:"logging"  env-prefix:"OGM_"`
	I18n appconf.I18n     `yaml:"i18n"     env-prefix:"OGM_"`
	DB   appconf.Database `yaml:"database" env-prefix:"OGM_"`
}

// DefaultDSN is used when no database DSN is configured: a SQLite file next to
// the binary's working directory.
const DefaultDSN = "file:open-go-menu.db"
