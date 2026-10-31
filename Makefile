# TrueSense developer shortcuts
.PHONY: test check install uninstall doctor module-test
test:            ## unit tests against the firmware simulator (no hardware needed)
	python3 -m unittest discover -s tests -t . -v
check: test      ## tests + shell syntax of every script
	@for f in tools/*.sh tools/truesense-fast packaging/health.sh; do bash -n $$f && echo "ok  $$f"; done
install:         ## one command: kernel module (DKMS), daemon, user services, completions
	sudo tools/install.sh
uninstall:
	sudo tools/uninstall.sh
doctor:
	./truesense doctor
module-test:     ## build the kernel module for every installed kernel (warnings on)
	@for k in $$(ls /lib/modules); do [ -e /lib/modules/$$k/build ] && { d=$$(mktemp -d) && cp kernel/truesense-wmi/truesense_wmi.c kernel/truesense-wmi/Makefile $$d/ && (cd $$d && make KVER=$$k W=1 >/dev/null 2>&1 && echo "built for $$k" || echo "FAILED for $$k"); rm -rf $$d; }; done
