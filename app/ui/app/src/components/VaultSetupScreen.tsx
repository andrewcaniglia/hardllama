import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LockClosedIcon } from "@heroicons/react/24/outline";
import { setupVault } from "@/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

const MIN_PASSWORD_LENGTH = 8;

export function VaultSetupScreen() {
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (password !== confirmation) {
      setError("Passwords don't match");
      return;
    }
    setIsSaving(true);
    setError("");
    try {
      const status = await setupVault(password);
      setPassword("");
      setConfirmation("");
      queryClient.setQueryData(["vault"], status);
      queryClient.invalidateQueries({ queryKey: ["chats"] });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to encrypt chats");
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="flex h-screen w-full items-center justify-center bg-white dark:bg-neutral-900">
      <form onSubmit={handleSubmit} className="w-80 space-y-3 text-center">
        <LockClosedIcon className="mx-auto h-8 w-8 text-neutral-500 dark:text-neutral-400" />
        <h1 className="text-lg font-medium text-neutral-900 dark:text-white">
          Create a password
        </h1>
        <p className="text-sm text-neutral-500 dark:text-neutral-400">
          Your chats are encrypted with this password, and HardLlama asks for it
          every time it opens.
        </p>
        <p className="text-sm text-red-600 dark:text-red-400">
          There is no password reset. If you forget it, your chats are gone for
          good.
        </p>
        <Input
          type="password"
          autoFocus
          placeholder="Password"
          aria-label="Password"
          value={password}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
            setPassword(e.target.value)
          }
        />
        <Input
          type="password"
          placeholder="Confirm password"
          aria-label="Confirm password"
          value={confirmation}
          onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
            setConfirmation(e.target.value)
          }
        />
        {error && (
          <p role="alert" className="text-sm text-red-600 dark:text-red-400">
            {error}
          </p>
        )}
        <Button
          type="submit"
          className="w-full"
          disabled={password.length < MIN_PASSWORD_LENGTH || isSaving}
        >
          {isSaving ? "Encrypting..." : "Encrypt chats"}
        </Button>
      </form>
    </div>
  );
}
