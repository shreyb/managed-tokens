VERSION=v0.1.0
ROOTDIR=`pwd`
BUILD=`date -u +%Y-%m-%dT%H:%M:%SZ`

all: build-all

build-all: refresh-uids-from-ferry run-onboarding-managed-tokens token-push

refresh-uids-from-ferry:
	cd cmd/refresh-uids-from-ferry;\
	go build -ldflags="-X main.buildTimestamp=${BUILD} -X main.version=${VERSION}";  \
	cd $${ROOTDIR}

run-onboarding-managed-tokens:
	cd cmd/run-onboarding-managed-tokens; \
	go build -ldflags="-X main.buildTimestamp=${BUILD} -X main.version=${VERSION}"; \
	cd $${ROOTDIR}

token-push:
	cd cmd/token-push; \
	go build -ldflags="-X main.buildTimestamp=${BUILD} -X main.version=${VERSION}"; \
	cd $${ROOTDIR}
