package worker

import (
	"context"
	"time"

	"github.com/shreyb/managed-tokens/utils"
	log "github.com/sirupsen/logrus"
)

func getProperTimeoutFromContext(ctx context.Context, defaultDuration string) (time.Duration, error) {
	var useTimeout time.Duration
	var ok bool
	var err error

	if useTimeout, ok = utils.GetOverrideTimeoutFromContext(ctx); !ok {
		log.Debug("No overrideTimeout set.  Will use default")
		useTimeout, err = time.ParseDuration(defaultDuration)
		if err != nil {
			log.Error("Could not parse default timeout duration")
			return useTimeout, err
		}
	}
	return useTimeout, nil
}
