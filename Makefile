NAME = managed-tokens
VERSION = v0.1.0
ROOTDIR = $(shell pwd)
BUILD = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
rpmVersion := $(subst v,,$(VERSION))
SOURCEDIR = $(NAME)-$(rpmVersion)
executables = refresh-uids-from-ferry run-onboarding-managed-tokens token-push
specfile := $(ROOTDIR)/packaging/$(NAME).spec

all: build tarball spec rpm
.PHONY: all clean build tarball spec rpm


rpm: rpmSourcesDir := $$HOME/rpmbuild/SOURCES
rpm: rpmSpecsDir := $$HOME/rpmbuild/SPECS
rpm: rpmDir := $$HOME/rpmbuild/RPMS/x86_64/
rpm: spec tarball
	cp $(specfile) $(rpmSpecsDir)/
	cp $(SOURCEDIR).tar.gz $(rpmSourcesDir)/
	cd $(rpmSpecsDir); \
	rpmbuild -ba ${NAME}.spec
	find $$HOME/rpmbuild/RPMS -type f -name "$(NAME)-$(rpmVersion)*.rpm" -cmin 1 -exec cp {} $(ROOTDIR)/ \;
	echo "Created RPM and copied it to current working directory"


spec:
	sed -Ei 's/Version\:[ ]*.+/Version:        $(rpmVersion)/' $(specfile)
	echo "Set version in spec file to $(rpmVersion)"


tarball: build
	mkdir -p $(SOURCEDIR)
	cp $(foreach exe,$(executables),cmd/$(exe)/$(exe)) $(SOURCEDIR)  # Executables
	cp $(ROOTDIR)/managedTokens.yml $(ROOTDIR)/packaging/managed-tokens.logrotate $(ROOTDIR)/packaging/managed-tokens.cron $(SOURCEDIR)  # Config files
	cp -r $(ROOTDIR)/templates/ $(SOURCEDIR)/templates
	tar -czf $(SOURCEDIR).tar.gz $(SOURCEDIR)
	echo "Built deployment tarball"


build:
	for exe in $(executables); do \
		echo "Building $$exe"; \
		cd cmd/$$exe;\
		go build -ldflags="-X main.buildTimestamp=${BUILD} -X main.version=${VERSION}";  \
		echo "Built $$exe"; \
		cd $(ROOTDIR); \
	done


clean:
# TODO Check for existence of file before deleting it
	rm $(SOURCEDIR).tar.gz
	rm -Rf $(SOURCEDIR)
	rm $(NAME)-$(rpmVersion)*.rpm
