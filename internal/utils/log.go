package utils

import (
	"os"
	"path/filepath"

	"github.com/chia-network/go-chia-libs/pkg/config"
	log "github.com/sirupsen/logrus"
)

// LogToFile logs a message to a given file
func LogToFile(filename, message string) error {
	rootPath, err := config.GetChiaRootPath()
	if err != nil {
		return err
	}
	path := filepath.Join(rootPath, "log", filename)
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Errorf("Error closing file: %s\n", err.Error())
		}
	}(file)

	if _, err := file.WriteString(message + "\n"); err != nil {
		return err
	}
	return nil
}
