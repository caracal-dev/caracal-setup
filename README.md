[![Codacy Badge](https://app.codacy.com/project/badge/Grade/edd1881e87164d0ba3962a9e6269ae2b)](https://app.codacy.com/gh/caracal-dev/caracal-setup/dashboard?utm_source=gh&utm_medium=referral&utm_content=&utm_campaign=Badge_grade)

# Caracal Setup

![Caracal Setup wizard](assets/images/screenshot-caracal-setup-ui.png)

`caracal-setup` is a Wails desktop wizard for the first graphical launch on Caracal OS and a reusable settings GUI for common system identity changes.

It mirrors the look and static frontend structure of `caracal-software-installer`, but focuses on the mandatory first-run flow:

- save a new hostname, username, or password
- launch `ujust first-run` in a terminal window
- finish with a reboot action

## Development

```bash
go run ./cmd/caracal-setup-gui
./scripts/wails-dev.sh
./scripts/wails-build.sh
```

Switch the packaged desktop icon by copying one of the PNGs in `build/icons/` to `build/appicon.png`:

```bash
./scripts/switch-app-icon
./scripts/switch-app-icon caracal-lakers.png
```

The frontend is a static bundle in `frontend/dist`, so `npm run build` only verifies that the generated assets exist.
