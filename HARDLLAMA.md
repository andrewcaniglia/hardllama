# HardLlama

HardLlama is an unofficial, hardened build of the Ollama desktop app for Apple Silicon Macs. It isn't affiliated with Ollama. The goal is that your chats can't be read from disk, even while the app is open.

The `ollama` server, models and CLI are unchanged. Only the desktop app is different.

## What's different

- **A password is required.** The app won't do anything until you create one, and it asks for it every time it opens.
- **Chats are encrypted on disk.** Each value is encrypted before it's saved, so the chat text in the database stays scrambled even while the app is open.
- **Private chats.** Click the lock button before your first message to keep a chat in memory only. Nothing is written to disk, and it's gone when the app quits or locks.
- **Deleted chats are overwritten** rather than just unlinked.
- **Updates are off.** The app doesn't check for, download or install updates. See [Updating](#updating).

## What's encrypted

| Encrypted | Not encrypted |
|---|---|
| Messages and model thinking | Number of chats, messages and attachments |
| Chat titles | How big each one is |
| Attachments and their filenames | Which messages belong to which chat |
| Tool calls, arguments and results | Other settings, such as window size |
| Timestamps | |
| Who sent each message | |
| Model names | |
| Browser state | |
| Selected model and working folder | |

The whole file can't be encrypted, because the `ollama` CLI reads this database and the app needs its settings before you can enter a password. The selected model and working folder read as empty until you unlock.

HardLlama doesn't save your ollama.com account details. The official app caches your name, email and plan; HardLlama deletes that cache.

Databases from earlier HardLlama versions only encrypted chat content. The first unlock with this version encrypts the rest and removes the old plain text from the file.

Encryption uses AES-256-GCM with a random key. That key is stored wrapped by a key derived from your password with Argon2id, and only exists unwrapped in memory while the app is unlocked.

## Locking

The app locks when you quit, lock your screen, your Mac sleeps, after 15 minutes idle, or when you click **Lock now** in Settings. Locking throws away the key and erases private chats. If it locks during a reply, the rest of that reply isn't saved.

**There is no password reset.** If you forget your password, your chats are gone for good.

To change your password, open **Settings** and click **Change password**. New passwords must be at least 12 characters. Changing it also replaces the encryption key and re-encrypts every chat, so an old copy of the database and your old password can't unlock anything you write afterwards.

## What this doesn't protect against

- Someone looking at your screen, or anything allowed to record it
- Root access or malware reading the app's memory while it's unlocked, or capturing your password as you type it
- Cloud models and web search, which still send your content off your device, even in private chats
- Anything you put in `~/.ollama/history`. Set `OLLAMA_NOHISTORY=1` to turn off CLI history.
- Models, and any system prompts saved in them, in `~/.ollama/models`. That includes sessions saved with `/save` in the CLI. The server reads these directly, so HardLlama can't encrypt them.
- `~/.ollama/id_ed25519`, the key that identifies you to ollama.com. Only your account can read it.
- Other programs on your Mac using the server on port 11439. It doesn't require a password, but it doesn't store your chats either.
- A weak password. Anyone with a copy of the database can keep guessing, so use a long one. Passwords set before the 12-character minimum keep working; change yours if it's shorter.

None of this has been independently audited.

## Logs

The app's log is in `~/.ollama/logs/hardllama-app.log`, readable only by you. It doesn't include chat IDs, message text, attachment names or tool arguments.

The server's log, which names the models you use and when, is kept in memory rather than written to disk. To write it to `~/.ollama/logs/hardllama-server.log` for debugging, start HardLlama with `HARDLLAMA_SERVER_LOG=1`:

```sh
HARDLLAMA_SERVER_LOG=1 open -a HardLlama
```

## Alongside the official app

HardLlama has its own identity, so it can be installed next to `Ollama.app`. Neither app closes the other, and installing HardLlama doesn't touch your existing Ollama chats.

| | HardLlama | Official Ollama |
|---|---|---|
| Bundle ID | `com.hardllama.app` | `com.electron.ollama` |
| Chats | `~/Library/Application Support/HardLlama` | `~/Library/Application Support/Ollama` |
| Server port | 11439 | 11434 |
| Logs | `~/.ollama/logs/hardllama-app.log` | `~/.ollama/logs/*.log` |

Some things are still shared:

- **Models.** Both use `~/.ollama/models`, so a model downloaded in one is available in the other.
- **The `ollama` command.** HardLlama only installs it if you don't already have one.
- **`ollama://` links.** Both apps register these, so a sign-in link may open the wrong one.

## Downloading models

Pick a model in the app and send a message, and it downloads as usual. To use the `ollama` command with HardLlama, point it at HardLlama's port:

```sh
export OLLAMA_HOST=127.0.0.1:11439
ollama pull gemma3
```

Add that `export` to `~/.zshrc` to make it permanent. Tools that assume port 11434 need the same setting.

Models come from Ollama's registry, so turning off app updates doesn't affect downloads. A brand new model may need a newer server, though. See [Updating](#updating).

## Building

HardLlama builds on Apple Silicon Macs (M1 or later) running macOS 14 or newer. You don't need Xcode or cmake.

1. Install Apple's Command Line Tools, which include the compiler and `git`:

   ```sh
   xcode-select --install
   ```

2. Install [Homebrew](https://brew.sh) if you don't have it, then Go and Node:

   ```sh
   brew install go node
   ```

3. Get the code:

   ```sh
   git clone https://github.com/andrewcaniglia/hardllama.git
   cd hardllama
   ```

4. Build it. The first time, pass the Ollama release to take the server from:

   ```sh
   scripts/build_hardllama.sh 0.40.14
   ```

   Once HardLlama is installed, you can leave the version out to reuse the installed server:

   ```sh
   scripts/build_hardllama.sh
   ```

5. Install it. Quit HardLlama first if it's running:

   ```sh
   rm -rf /Applications/HardLlama.app && ditto dist/HardLlama.app /Applications/HardLlama.app
   ```

6. Open HardLlama from Applications and create your password.

The script only compiles the app. The server and its libraries come from the official Ollama release, and the script checks that release is signed by Ollama before using it.

The app is ad-hoc signed, not signed by a registered developer. A copy built on your own Mac opens normally. If you copy it to another Mac, macOS will refuse to open it at first. Try to open it once, then go to **System Settings → Privacy & Security**, scroll down and click **Open Anyway**. Building it on each Mac avoids this.

## Updating

Nothing updates by itself, including security fixes, so watch [Ollama's releases](https://github.com/ollama/ollama/releases).

**Server updates** bring new models, speed improvements and most fixes. Our changes don't touch the server, so pass the new version to the build script.

**App updates** mean replaying our changes on top of Ollama's code:

```sh
git fetch origin
git rebase origin/main
```

Then run the tests and rebuild:

```sh
go test ./app/...
(cd app/ui/app && npx vitest run)
```

### Schema version trap

This build uses database schema version 20 for the `vault` table. If Ollama ships its own version 20, databases already at 20 would skip their migration. Don't just renumber: resolve it so that both changes are applied to existing HardLlama databases.

Any new upstream column that stores chat content or details about chats must also be added to `sealedColumns` in `app/store/vault.go`, and its writes must go through `seal` (or `sealTime` for timestamps), or it will be saved unencrypted. Reads of timestamp columns must `CAST` them to text, or the SQLite driver turns encrypted values into the zero time. `assertAllSealed` in `vault_details_test.go` checks every listed column.
