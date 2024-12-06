NAME = managed-tokens
VERSION = v0.15.0
ROOTDIR = $(shell pwd)
BUILD = $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
rpmVersion := $(subst v,,$(VERSION))
buildTarName = $(NAME)-$(rpmVersion)
buildTarPath = $(ROOTDIR)/$(buildTarName).tar.gz
SOURCEDIR = $(ROOTDIR)/$(buildTarName)
executables = refresh-uids-from-ferry token-push
specfile := $(ROOTDIR)/packaging/$(NAME).spec
ifdef RACE
raceflag := -race
else
raceflag :=
endif

all: build tarball spec rpm
.PHONY: all clean clean-all build tarball spec rpm

rpm: rpmSourcesDir := $$HOME/rpmbuild/SOURCES
rpm: rpmSpecsDir := $$HOME/rpmbuild/SPECS
rpm: rpmDir := $$HOME/rpmbuild/RPMS/x86_64/
rpm: spec tarball
	cp $(specfile) $(rpmSpecsDir)/
	cp $(buildTarPath) $(rpmSourcesDir)/
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
	tar -czf $(buildTarPath) -C $(ROOTDIR) $(buildTarName)
	echo "Built deployment tarball"


build:
	for exe in $(executables); do \
		echo "Building $$exe"; \
		cd cmd/$$exe;\
		go build $(raceflag) -ldflags="-X main.buildTimestamp=$(BUILD) -X main.version=$(VERSION)";  \
		echo "Built $$exe"; \
		cd $(ROOTDIR); \
	done


clean-all: clean
	(test -e $(ROOTDIR)/$(NAME)-$(rpmVersion)*.rpm) && (rm $(ROOTDIR)/$(NAME)-$(rpmVersion)*.rpm)


clean:
	(test -e $(buildTarPath)) && (rm $(buildTarPath))
	(test -e $(SOURCEDIR)) && (rm -Rf $(SOURCEDIR))
