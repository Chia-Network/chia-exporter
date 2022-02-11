package utils

import "log"

// LogErr logs an error if there's an error and the continues
func LogErr(_, _ interface{}, err error) {
	if err != nil {
		log.Printf("Error requesting connections: %s\n", err.Error())
	}
}
