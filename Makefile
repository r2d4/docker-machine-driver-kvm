
# The binary to build (just the basename).
BIN := docker-machine-driver-kvm

# This repo's root import path (under GOPATH).
PKG := github.com/r2d4/docker-machine-driver-kvm

# Where to push the docker image.
REGISTRY ?= r2d4

# Which architecture to build - see $(ALL_ARCH) for options.
ARCH ?= amd64

# This version-strategy uses git tags to set the version string
#VERSION := $(shell git describe --tags --always --dirty)
#
# This version-strategy uses a manual value to set the version string
VERSION := v0.0.1

###
### These variables should not need tweaking.
###

# directories which hold app source (not vendored)
SRC_DIRS := cmd pkg 

ALL_ARCH := amd64

BASEIMAGE?=alpine

IMAGE := $(REGISTRY)/$(BIN)-$(ARCH)

BUILD_IMAGE ?= $(PKG)-build

# If you want to build all binaries, see the 'all-build' rule.
# If you want to build all containers, see the 'all-container' rule.
# If you want to build AND push all containers, see the 'all-push' rule.
all: build

install: all
	sudo mv ./bin/amd64/docker-machine-driver-kvm /usr/local/bin/docker-machine-driver-kvm

build-%:
	@$(MAKE) --no-print-directory ARCH=$* build

container-%:
	@$(MAKE) --no-print-directory ARCH=$* container

push-%:
	@$(MAKE) --no-print-directory ARCH=$* push

all-build: $(addprefix build-, $(ALL_ARCH))

all-container: $(addprefix container-, $(ALL_ARCH))

all-push: $(addprefix push-, $(ALL_ARCH))

build: bin/$(BIN)

build-container: Dockerfile.build 
	@docker build -t $(BUILD_IMAGE) -f Dockerfile.build .

bin/docker/$(BIN): build-dirs build-container
	@echo "building: $@"
	@docker run                                                            \
	    -ti                                                                \
	    -u $$(id -u):$$(id -g)                                             \
	    -v $$(pwd)/.go:/go                                                 \
	    -v $$(pwd):/go/src/$(PKG)                                          \
	    -v $$(pwd)/bin:/go/bin                      	               \
	    -v $$(pwd)/bin:/go/bin/linux		                       \
	    -v $$(pwd)/.go/std:/usr/local/go/pkg/linux_static		       \
	    -w /go/src/$(PKG)                                                  \
	    $(BUILD_IMAGE)                                                     \
	    /bin/sh -c "                                                       \
	        VERSION=$(VERSION)                                             \
	        PKG=$(PKG)                                                     \
	        ./build/build.sh                                               \
	    "

bin/$(BIN): build-dirs
	/bin/sh -c "				\
		VERSION=$(VERSION)		\
		PKG=$(PKG)			\
		./build/build.sh		\
	"

DOTFILE_IMAGE = $(subst /,_,$(IMAGE))-$(VERSION)

container: .container-$(DOTFILE_IMAGE) container-name
.container-$(DOTFILE_IMAGE): bin/$(ARCH)/$(BIN) Dockerfile.in
	@sed \
	    -e 's|ARG_BIN|$(BIN)|g' \
	    -e 's|ARG_ARCH|$(ARCH)|g' \
	    -e 's|ARG_FROM|$(BASEIMAGE)|g' \
	    Dockerfile.in > .dockerfile-$(ARCH)
	@docker build -t $(IMAGE):$(VERSION) -f .dockerfile-$(ARCH) .
	@docker images -q $(IMAGE):$(VERSION) > $@

container-name:
	@echo "container: $(IMAGE):$(VERSION)"

push: .push-$(DOTFILE_IMAGE) push-name
.push-$(DOTFILE_IMAGE): .container-$(DOTFILE_IMAGE)
	@gcloud docker push $(IMAGE):$(VERSION)
	@docker images -q $(IMAGE):$(VERSION) > $@

push-name:
	@echo "pushed: $(IMAGE):$(VERSION)"

version:
	@echo $(VERSION)

test: build-dirs
	@docker run                                                            \
	    -ti                                                                \
	    -u $$(id -u):$$(id -g)                                             \
	    -v $$(pwd)/.go:/go                                                 \
	    -v $$(pwd):/go/src/$(PKG)                                          \
	    -v $$(pwd)/bin/$(ARCH):/go/bin                                     \
	    -v $$(pwd)/.go/std/$(ARCH):/usr/local/go/pkg/linux_$(ARCH)_static  \
	    -w /go/src/$(PKG)                                                  \
	    $(BUILD_IMAGE)                                                     \
	    /bin/sh -c "                                                       \
	        ./build/test.sh $(SRC_DIRS)                                    \
	    "

build-dirs:
	@mkdir -p bin/$(ARCH)
	@mkdir -p .go/src/$(PKG) .go/pkg .go/bin .go/std/$(ARCH)

clean: container-clean bin-clean

container-clean:
	rm -rf .container-* .dockerfile-* .push-*

bin-clean:
	rm -rf .go bin
