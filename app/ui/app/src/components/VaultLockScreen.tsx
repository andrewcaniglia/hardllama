import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LockClosedIcon } from "@heroicons/react/24/outline";
import { unlockVault } from "@/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function VaultLockScreen() {
  const queryClient = useQueryClient();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [isUnlocking, setIsUnlocking] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setIsUnlocking(true);
    setError("");
    try {
      const status = await unlockVault(password);
      setPassword("");
      queryClient.setQueryData(["vault"], status);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to unlock");
    } finally {
      setIsUnlocking(false);
    }
  };

  return (
    <div className="flex h-screen w-full items-center justify-center bg-white dark:bg-neutral-900">
      <form onSubmit={handleSubmit} className="w-72 space-y-3 text-center">
        <LockClosedIcon className="mx-auto h-8 w-8 text-neutral-500 dark:text-neutral-400" />
        <h1 className="text-lg font-medium text-neutral-900 dark:text-white">
          Your chats are locked
        </h1>
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
        {error && (
          <p role="alert" className="text-sm text-red-600 dark:text-red-400">
            {error}
          </p>
        )}
        <Button
          type="submit"
          className="w-full"
          disabled={!password || isUnlocking}
        >
          {isUnlocking ? "Unlocking..." : "Unlock"}
        </Button>
      </form>
    </div>
  );
}
