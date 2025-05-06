package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const configFileName = ".gatorconfig.json"

type Config struct {
	DbUrl           string `json:"db_url"`
	CurrentUserName string `json:"current_user_name"`
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, configFileName), nil
}

func Read() (Config, error) {
	var cfg Config
	file, err := getConfigPath()
	if err != nil {
		return cfg, err
	}
	openfile, err := os.Open(file)
	if err != nil {
		return cfg, err
	}
	defer openfile.Close()
	decoder := json.NewDecoder(openfile)
	err = decoder.Decode(&cfg)
	if err != nil {
		return cfg, err
	}
	return cfg, nil
}

func write(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	filepath, err := getConfigPath()
	if err != nil {
		return err
	}
	f, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func (cfg *Config) SetUser(user string) error {
	cfg.CurrentUserName = user
	err := write(*cfg)
	if err != nil {
		return err
	}
	return nil
}
