# TrueSense developer shortcuts
.PHONY: test check lint install uninstall doctor module-test ci release-check
test:            ## unit tests against the firmware simulator (no hardware needed)
	python3 -m unittest discover -s tests -t . -v
lint:            ## static checks (ruff: real bugs, unused code); skipped when ruff is not installed
	@if command -v ruff >/dev/null 2>&1; then ruff check . && echo "ok  ruff"; else echo "skip ruff (not installed: pip install ruff)"; fi
check: test lint ## tests + lint + shell syntax of every script
	@for f in tools/*.sh tools/ci/*.sh tools/truesense-fast packaging/health.sh; do bash -n $$f && echo "ok  $$f"; done
ci:              ## what CI's static job runs, locally (tools that are not installed are skipped): the gate before you push
	@python3 tools/release.py check
	@if command -v shellcheck >/dev/null 2>&1; then shellcheck -S warning -x tools/*.sh tools/ci/*.sh tools/truesense-fast packaging/health.sh && echo "ok  shellcheck"; else echo "skip shellcheck"; fi
	@if command -v actionlint >/dev/null 2>&1; then actionlint && echo "ok  actionlint"; else echo "skip actionlint"; fi
	@tools/ci/verify-units.sh | tail -3
	@python3 tools/ci/check-links.py
	@$(MAKE) --no-print-directory check
release-check:   ## do the version numbers, notes and tag agree?  (make release-check TAG=v0.3.16)
	python3 tools/release.py check $(if $(TAG),--tag $(TAG),)
install:         ## one command: kernel module (DKMS), daemon, user services, completions
	sudo tools/install.sh
uninstall:
	sudo tools/uninstall.sh
doctor:
	./truesense doctor
module-test:     ## build the kernel module for every installed kernel (warnings on)
	@tools/ci/build-modules.sh
