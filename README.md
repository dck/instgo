# instgo

Instagram direct messages in your terminal.

instgo talks to Instagram's private mobile API and shows your inbox and conversations in a two-pane terminal UI. It reads and sends text messages; photos, videos, voice messages and shared posts are shown as placeholders like `[photo]`.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/dck/instgo@latest
```

Or build from a checkout:

```sh
go build -o instgo .
./instgo
```

## Logging in

instgo offers two ways to log in:

- **Username and password.** Enter both, plus a verification code if Instagram asks for one. Instagram may refuse fresh password logins from third-party clients; if it does, use the session cookie instead.
- **Browser session cookie.** Press `ctrl+b` on the login screen, then paste the `sessionid` cookie from a browser where you are logged in to instagram.com (in Firefox: F12 → Storage → Cookies → `sessionid` → copy the value).

The session is saved, so you only log in once.

## Keys

| Where | Key | Action |
| --- | --- | --- |
| Chat list | `j` / `k`, `↑` / `↓` | Move between conversations |
| Chat list | `g` / `G` | Jump to top / bottom |
| Chat list | `enter`, `l`, `tab`, `i` | Write in the open conversation |
| Chat list | `/` | Filter conversations by name |
| Chat list | `r` | Refresh the inbox |
| Chat list | `q` | Quit |
| Message box | `enter` | Send |
| Message box | `esc`, `tab` | Back to the chat list |
| Message box | `↑` / `↓` | Scroll messages |
| Message box | `ctrl+r` | Retry failed messages |
| Anywhere in chat | `pgup` / `pgdn`, `ctrl+u` / `ctrl+d` | Scroll messages by half a page |
| Anywhere in chat | `ctrl+x` | Replace all words on screen with decoy words; press again to show the real text |
| Everywhere | `ctrl+c` | Quit |

The mouse works too: click a conversation to open it, scroll with the wheel.

The real text is also hidden automatically, as if you pressed `ctrl+x`, after 20 seconds without a key press or mouse action, and as soon as the terminal window loses focus. Focus detection needs a terminal that reports focus changes (in tmux, `set -g focus-events on`). Press `ctrl+x` to show the text again.

## Files

instgo keeps its state in `$XDG_CONFIG_HOME/instgo/` (default `~/.config/instgo/`):

- `session.json` holds your login session. Treat it like a password.
- `debug.log` records API requests and responses with tokens, cookies and passwords redacted.
- `debug-<step>.json` holds Instagram's redacted reply when a login step fails.

## Disclaimer

instgo is an unofficial client and is not affiliated with or endorsed by Instagram or Meta. Using the private API is against Instagram's terms of use, and Instagram may rate-limit, challenge or suspend accounts that use third-party clients. Use it at your own risk.

## Acknowledgements

- [instagrapi](https://github.com/subzeroid/instagrapi): the Instagram API client in `ig/` is a Go port of its request flow. See [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES) for its license.
- [instagram-cli](https://github.com/supreme-gg-gg/instagram-cli): inspired the idea of a terminal client for Instagram DMs.

## License

[MIT](LICENSE)
