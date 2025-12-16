package secret

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"sync"
)

var lock sync.RWMutex
var secretsMap map[string][]byte

// LoadSecrets loads multiple paths of secrets and add them in a map.
func LoadSecrets(paths []string) error {
	lock.Lock()
	defer lock.Unlock()
	secrets := make(map[string][]byte)
	for _, path := range paths {
		secretValue, err := loadSingleSecret(path)
		if err != nil {
			return err
		}
		secrets[path] = secretValue
	}
	secretsMap = secrets
	return nil
}

// loadSingleSecret reads and returns the value of a single file.
func loadSingleSecret(path string) ([]byte, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %v", path, err)
	}
	return bytes.TrimSpace(b), nil
}

// GetSecret returns the value of a secret stored in a map.
func GetSecret(secretPath string) []byte {
	lock.RLock()
	defer lock.RUnlock()
	return secretsMap[secretPath]
}

// GetGenerator returns a function that gets the value of a given secret.
func GetGenerator(secretPath string) func() []byte {
	return func() []byte {
		return GetSecret(secretPath)
	}
}
