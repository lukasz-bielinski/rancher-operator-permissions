package controllers

import (
	"encoding/json"
	"os"
)

func readFileIfExists(filename string) ([]byte, error) {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// file does not exist
		return nil, err
	}
	return os.ReadFile(filename)
}

func parseRoleTemplatesFile(data []byte) ([]struct {
	substring    string
	roleTemplate string
}, error) {
	var roleTemplates []struct {
		substring    string
		roleTemplate string
	}

	err := json.Unmarshal(data, &roleTemplates)
	if err != nil {
		return nil, err
	}
	return roleTemplates, nil
}
