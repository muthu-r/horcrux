PLUGIN_REPO = muthur
PLUGIN_NAME = horcrux
PLUGIN_REPO_NAME = ${PLUGIN_REPO}/${PLUGIN_NAME}

MAKEFLAGS += --no-print-directory

all: cli plugin

FORCE:

plugin: FORCE
	@echo "Building Horcrux Docker volume plugin"
	@echo "Building Plugin rootfs"
	@mkdir -p plugin/rootfs
	@docker build -q -t ${PLUGIN_REPO_NAME}:rootfs .
	@docker create --name ${PLUGIN_NAME}.tmp ${PLUGIN_REPO_NAME}:rootfs
	@docker export ${PLUGIN_NAME}.tmp | tar -x -C ./plugin/rootfs
	@mkdir -p plugin/rootfs/run/horcrux
	@cp config.json plugin
	@cp plugin/rootfs/go/bin/horcrux-dv plugin/rootfs
	@docker rm ${PLUGIN_NAME}.tmp
	@docker plugin rm -f ${PLUGIN_REPO_NAME} || true
	@docker plugin create ${PLUGIN_REPO_NAME} ./plugin
	@docker plugin enable ${PLUGIN_REPO_NAME}
	@rm -rf ./plugin
	@docker image rm -f ${PLUGIN_REPO_NAME}:rootfs || true

clean-plugin:
	@docker plugin disable ${PLUGIN_REPO_NAME} || true
	@docker plugin rm -f ${PLUGIN_REPO_NAME} || true
	@docker image rm -f ${PLUGIN_REPO_NAME}:rootfs || true

cli: FORCE
	@echo "Building Horcrux CLI"
	@make -C horcrux-cli

install: install-cli

install-cli: FORCE
	@echo "Installing Horcrux CLI"
	@make -C horcrux-cli install

clean:
	@echo "Cleaning Horcrux CLI"
	@make -C horcrux-cli clean
	@echo "Cleaning plugin dir"
	@rm -rf ./plugin
