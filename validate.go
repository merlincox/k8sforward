package k8sforward

import (
	"fmt"
	"strconv"
)

func validateNonEmptyString(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must be specified", name)
	}
	return nil
}

func validateTCPPort(name, portStr string) error {
	if err := validateNonEmptyString(name, portStr); err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err == nil && port >= 0 && port <= 65535 {
		return nil
	}
	return fmt.Errorf("%s must be an integer from 0 to 65535 but was '%s'", name, portStr)
}
