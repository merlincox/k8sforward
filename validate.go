package k8sforward

import (
	"fmt"
	"strconv"
	"strings"
)

func validateNonEmptyString(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s must be a non-empty string", name)
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

func validateLocalAddress(localAddress string) ([]string, error) {
	if err := validateNonEmptyString("local address", localAddress); err != nil {
		return nil, err
	}
	addressParts := strings.Split(localAddress, ":")
	if len(addressParts) != 2 {
		return nil, fmt.Errorf("local address must be in host:port format but was '%s'", localAddress)
	}
	if err := validateNonEmptyString("local host", addressParts[0]); err != nil {
		return nil, err
	}
	if err := validateTCPPort("local port", addressParts[1]); err != nil {
		return nil, err
	}
	return addressParts, nil
}
