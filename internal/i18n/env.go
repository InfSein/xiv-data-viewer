package i18n

import "os"

func envVar(key string) string {
	return os.Getenv(key)
}
