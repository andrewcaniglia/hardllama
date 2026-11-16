# HardLlama

HardLlama is an unofficial, hardened build of the Ollama desktop app for Apple Silicon Macs. It isn't affiliated with Ollama. The goal is that your chats can't be read from disk, even while the app is open.

The `ollama` server, models and CLI are unchanged. Only the desktop app is different.

## What's different

- **A password is required.** The app won't do anything until you create one, and it asks for it every time it opens.
- **Chats are encrypted on disk.** Each value is encrypted before it's saved, so the chat text in the database stays scrambled even while the app is open.
- **Private chats.** Click the lock button before your first message to keep a chat in memory only. Nothing is written to disk, and it's gone when the app quits or locks.
- **Deleted chats are overwritten** rather than just unlinked.
- **Updates are off.** The app doesn't check for, download or install updates. See [Updating](#updating).
- **Nothing leaves your computer by accident.** Ollama's paid cloud models, web search and ollama.com sign-in are gone, and the app doesn't contact ollama.com when it starts. Only downloading models, and checking whether a model has an update, talk to ollama.com.
- **A context counter.** Under the message box, it shows how many tokens the chat, your message and its attachments will use, against the context length. The chat so far is counted exactly by the model after each reply. What you haven't sent yet is estimated, marked ≈. It turns amber when nearly full, and red when the model won't see the oldest messages, with a link to raise the context length in Settings. Raising it brings them back for the next message.
- **No connections to other companies' AI apps.** The Apps page, the ChatGPT and Claude Desktop integrations, their installers, the Claude Desktop gateway and their menu bar items are gone.

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
| Token counts | |

The whole file can't be encrypted, because the `ollama` CLI reads this database and the app needs its settings before you can enter a password. The selected model and working folder read as empty until you unlock.

HardLlama doesn't sign in to ollama.com or save account details. The official app caches your name, email and plan; HardLlama deletes that cache.

Databases from earlier HardLlama versions only encrypted chat content. The first unlock with this version encrypts the rest and removes the old plain text from the file.

Encryption uses AES-256-GCM with a random key. That key is stored wrapped by a key derived from your password with Argon2id, and only exists unwrapped in memory while the app is unlocked.

## Locking

The app locks when you quit, lock your screen, your Mac sleeps, after 15 minutes without typing or clicking in it, or when you click **Lock now** in Settings. It also locks shortly after you switch to another app or close its window, unless you come back first. Choose how long, from instantly to 1 minute, under **Lock after leaving** in Settings.

Locking throws away the key and erases private chats. If it locks during a reply, the reply keeps going. The rest of it is encrypted as it's written, held in memory only, and added to the chat when you unlock. If you quit before unlocking, it's lost. A reply stops if it needs a tool while locked, and private chats' replies stop, since locking erases them. A message you were still typing is kept, encrypted and in memory only, and put back when you unlock. Its attachments aren't kept.

**There is no password reset.** If you forget your password, your chats are gone for good.

To change your password, open **Settings** and click **Change password**. New passwords must be at least 12 characters. Changing it also replaces the encryption key and re-encrypts every chat, so an old copy of the database and your old password can't unlock anything you write afterwards.

## What this doesn't protect against

- Someone looking at your screen, or anything allowed to record it
- Root access or malware reading the app's memory while it's unlocked, or capturing your password as you type it
- Anything you put in `~/.ollama/history`. Set `OLLAMA_NOHISTORY=1` to turn off CLI history.
- Models, and any system prompts saved in them, in `~/.ollama/models`. That includes sessions saved with `/save` in the CLI. The server reads these directly, so HardLlama can't encrypt them.
- `~/.ollama/id_ed25519`, the key that identifies you to ollama.com. HardLlama doesn't use it, but the official app and `ollama` command can. Only your account can read it.
- Other programs on your Mac using the server on port 11439. It doesn't require a password, but it doesn't store your chats either.
- A weak password. Anyone with a copy of the database can keep guessing, so use a long one. Passwords set before the 12-character minimum keep working; change yours if it's shorter.

None of this has been independently audited.

## Running models on another computer

HardLlama can run models on another computer you own, such as one with a faster GPU, while your chats stay saved and encrypted on this one. The other computer must be running HardLlama too. It only sees each prompt while it works on it, and never sees your saved chats.

### Setting it up

1. On the computer with the GPU, open **Settings**, turn on **Share models with my devices**, and click **Pair a device**. It shows a pairing code and the addresses other devices can use.
2. On this computer, open **Settings → Run models on another computer**, enter one of those addresses and the code, and click **Pair**.

The code works once and expires after 5 minutes. To go back to running models here, click **Use this computer**. To cut a device off, click **Remove** next to it on the sharing computer.

The first time you turn sharing on, macOS or Windows may ask whether HardLlama can accept incoming connections. Allow it, or other devices can't connect.

### How it stays private

The two computers talk over an encrypted connection using a key the sharing computer makes for itself. While pairing, each side proves it knows the code and checks the key the other side really has. After that, this computer only connects to that exact key, and the sharing computer only answers devices it paired.

So the connection stays private on any network, including public Wi-Fi or a VPN. Nobody in between, such as someone on the same network or your VPN provider, can read your prompts or pretend to be the other computer.

Paired devices can only list, download and run models. They can't delete models or see the sharing computer's chats.

### Reaching the other computer

The sharing computer listens on port 11443. On the same network, use its local address. From elsewhere, you need some way to reach it, such as forwarding port 11443 on your router to it, or your own WireGuard tunnel. Either way, the connection is encrypted by HardLlama itself.

### What to know

- Sharing keeps working while the sharing computer is locked, so it can sit at home and serve models. Turning sharing on or off, pairing and removing devices all need it unlocked.
- Pairing details are encrypted with your chats. While this computer is locked, nothing is sent to the other one.
- The model list, and downloads started from a chat, are the other computer's.
- The sharing computer keeps its key in `share-identity.pem` in its HardLlama folder, readable only by you. If it's deleted, paired devices refuse to connect until you pair them again.
- **Expose Ollama to the network** is gone from Settings and ignored if it was set. It opened the server to your whole network with no password.

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
- **`ollama://` links.** Both apps register these. In HardLlama they just open the app.

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
