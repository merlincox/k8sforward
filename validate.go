package k8sforward

import (
	"fmt"
	"strconv"
)

// ValidateFlags validates `namespace` and `appName` to be non-empty strings and `localPort` and `remotePort` to be
// valid TCP port number strings.
func ValidateFlags(namespace, appName, localPort, remotePort string) error {
	if err := validateNonEmptyString("k8s namespace", namespace); err != nil {
		return err
	}
	if err := validateNonEmptyString("k8s app name", appName); err != nil {
		return err
	}
	if err := validateTCPPort("local TCP port", localPort); err != nil {
		return err
	}
	if err := validateTCPPort("remote TCP port", remotePort); err != nil {
		return err
	}
	return nil
}

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
