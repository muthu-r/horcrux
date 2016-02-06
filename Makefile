MAKEFLAGS += --no-print-directory

all: cli dv

FORCE:

cli: FORCE
	@echo "Building Horcrux CLI"
	@make -C horcrux-cli

dv: FORCE
	@echo "Building Horcrux Docker volume plugin"
	@make -C horcrux-dv

install-cli: FORCE
	@echo "Installing Horcrux CLI"
	@make -C horcrux-cli install

install-dv: FORCE
	@echo "Installing Horcrux Docker volume plugin"
	@make -C horcrux-dv install

install: install-cli install-dv

clean:
	@echo "Cleaning Horcrux CLI"
	@make -C horcrux-cli clean
	@echo "Cleaning Horcrux Docker volume plugin"
	@make -C horcrux-dv clean
