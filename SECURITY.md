# Security

TrueSense installs a root service, a kernel module and a UNIX socket, so please report problems responsibly.

**Please do not open a public issue for a vulnerability.** Use GitHub's private vulnerability reporting for this repository (Security tab, "Report a vulnerability"), or contact the maintainer privately.

What is in scope: privilege escalation through the control socket or the key service, unsafe file handling in the installer or service, the kernel module accepting values it should refuse, anything that could damage hardware through the documented interfaces.

How it is built:
- The control socket (`/run/truesense/truesense.sock`) checks the caller's user id with `SO_PEERCRED`; only root and the configured user are served.
- The service runs under systemd sandboxing (`ProtectSystem`, `ProtectControlGroups`, `RestrictNamespaces`, ...), with `Restart=always` and a watchdog.
- `exec:` key actions run as the logged-in user, never as root.
- The kernel module whitelists commands, validates every argument and refuses unsupported machines unless forced.
- Settings written by the service are validated; profiles are plain JSON read with strict validation.

Supported version: the latest release.
