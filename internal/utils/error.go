package utils

import log "github.com/sirupsen/logrus"

// LogErr logs an error if there's an error and the continues
func LogErr(_, _ interface{}, err error) {
	if err != nil {
		log.Errorf("%s\n", err.Error())
	}
}
