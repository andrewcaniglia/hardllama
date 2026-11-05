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
| Messages and model thinking | Number of chats and messages |
| Chat titles | Timestamps |
| Attachments and their filenames | Who sent each message |
| Tool arguments and results | Model names and tool names |
| Browser state | Settings |

The whole file can't be encrypted, because the `ollama` CLI reads this database and the app needs its settings before you can enter a password.

Encryption uses AES-256-GCM with a random key. That key is stored wrapped by a key derived from your password with Argon2id, and only exists unwrapped in memory while the app is unlocked.

## Locking

The app locks when you quit, lock your screen, your Mac sleeps, after 15 minutes idle, or when you click **Lock now** in Settings. Locking throws away the key and erases private chats. If it locks during a reply, the rest of that reply isn't saved.

**There is no password reset.** If you forget your password, your chats are gone for good. There's also no way to change it yet.

## What this doesn't protect against

- Someone looking at your screen, or anything allowed to record it
- Root access or malware reading the app's memory while it's unlocked, or capturing your password as you type it
- Cloud models and web search, which still send your content off your device, even in private chats
- Anything you put in `~/.ollama/history`. Set `OLLAMA_NOHISTORY=1` to turn off CLI history.

## Don't run the official app

Uninstall the official `Ollama.app`. It doesn't know about encryption, so it would show scrambled text and save new chats unencrypted. It also closes HardLlama if it starts, because the newest launch wins.

## Building

You need Go and Node, but not Xcode or cmake:

```sh
brew install go node
```

From the repository root:

```sh
scripts/build_hardllama.sh            # rebuild, keeping the installed server
scripts/build_hardllama.sh 0.41.0     # rebuild with the server from that release
```

This only compiles the app. The server and its libraries come from an official release, which must be signed by Ollama. The build goes to `dist/HardLlama.app`, and the script prints the install command. Quit HardLlama before installing.

The app is ad-hoc signed, so it can only run on the Mac that built it unless macOS is told to allow it.

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

Any new upstream column that stores chat content must also be added to `sealedColumns` in `app/store/vault.go`, and its writes must go through `seal`, or it will be saved unencrypted.
