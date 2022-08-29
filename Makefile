NAME = managed-tokens
VERSION = v0.1.0
ROOTDIR = $(shell pwd)
BUILD = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SOURCEDIR = $(NAME)-$(VERSION)
executables = refresh-uids-from-ferry run-onboarding-managed-tokens token-push
specfile := $(ROOTDIR)/packaging/$(NAME).spec

all: build tarball spec
.PHONY: all clean build tarball spec

# TODO rpm target

build:
	for exe in $(executables); do \
		echo "Building $$exe"; \
		cd cmd/$$exe;\
		go build -ldflags="-X main.buildTimestamp=${BUILD} -X main.version=${VERSION}";  \
		cd $(ROOTDIR); \
	done

tarball: build
	mkdir -p $(SOURCEDIR)
	cp $(foreach exe,$(executables),cmd/$(exe)/$(exe)) $(SOURCEDIR)  # Executables
	cp $(ROOTDIR)/managedTokens.yml $(ROOTDIR)/packaging/managed-tokens.logrotate $(ROOTDIR)/packaging/managed-tokens.cron $(SOURCEDIR)  # Config files
	cp -r $(ROOTDIR)/templates/ $(SOURCEDIR)/templates
	tar -czf $(SOURCEDIR).tar.gz $(SOURCEDIR)


spec: rpmVersion := $(subst v,,$(VERSION))
spec:
	sed -Ei 's/Version\:[ ]*.+/Version:        $(rpmVersion)/' $(specfile)


clean:
	rm $(SOURCEDIR).tar.gz
	rm -Rf $(SOURCEDIR)
