import { useState, type FormEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { LockClosedIcon } from "@heroicons/react/24/outline";
import { changeVaultPassword, lockVault } from "@/api";
import { Button } from "@/components/ui/button";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";
import { MIN_VAULT_PASSWORD_LENGTH, shortPasswordMessage } from "@/lib/vault";

export function VaultSettings() {
  const queryClient = useQueryClient();
  const [isChanging, setIsChanging] = useState(false);
  const [current, setCurrent] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const handleLock = async () => {
    queryClient.setQueryData(["vault"], await lockVault());
  };

  const reset = () => {
    setCurrent("");
    setPassword("");
    setConfirmation("");
    setError("");
  };

  const handleCancel = () => {
    reset();
    setIsChanging(false);
  };

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setMessage("");
    if (password.length < MIN_VAULT_PASSWORD_LENGTH) {
      setError(shortPasswordMessage);
      return;
    }
    if (password !== confirmation) {
      setError("New passwords don't match");
      return;
    }
    setIsSaving(true);
    setError("");
    try {
      queryClient.setQueryData(
        ["vault"],
        await changeVaultPassword(current, password),
      );
      reset();
      setIsChanging(false);
      setMessage("Password changed");
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to change password",
      );
    } finally {
      setIsSaving(false);
    }
  };

  const field = (
    label: string,
    value: string,
    set: (value: string) => void,
    autoFocus = false,
  ) => (
    <Input
      type="password"
      autoFocus={autoFocus}
      placeholder={label}
      aria-label={label}
      value={value}
      onChange={(e: React.ChangeEvent<HTMLInputElement>) => set(e.target.value)}
    />
  );

  return (
    <Field>
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start space-x-3 flex-1">
          <LockClosedIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
          <div>
            <Label>Chat encryption</Label>
            <Description>
              Chats are encrypted on disk. They lock when you quit, lock your
              screen, leave HardLlama, or after 15 minutes idle.
            </Description>
            {message && (
              <p
                role="status"
                className="mt-1 text-sm text-neutral-500 dark:text-neutral-400"
              >
                {message}
              </p>
            )}
          </div>
        </div>
        <div className="flex flex-shrink-0 gap-2">
          {!isChanging && (
            <Button
              type="button"
              plain
              onClick={() => {
                setMessage("");
                setIsChanging(true);
              }}
            >
              Change password
            </Button>
          )}
          <Button type="button" onClick={handleLock}>
            Lock now
          </Button>
        </div>
      </div>
      {isChanging && (
        <form onSubmit={handleSubmit} className="mt-3 ml-8 max-w-sm space-y-2">
          {field("Current password", current, setCurrent, true)}
          {field(
            `New password (at least ${MIN_VAULT_PASSWORD_LENGTH} characters)`,
            password,
            setPassword,
          )}
          {field("Confirm new password", confirmation, setConfirmation)}
          <p className="text-sm text-neutral-500 dark:text-neutral-400">
            There is no password reset. If you forget the new one, your chats
            are gone for good.
          </p>
          {error && (
            <p role="alert" className="text-sm text-red-600 dark:text-red-400">
              {error}
            </p>
          )}
          <div className="flex gap-2">
            <Button type="submit" disabled={isSaving}>
              {isSaving ? "Changing..." : "Change password"}
            </Button>
            <Button type="button" plain onClick={handleCancel}>
              Cancel
            </Button>
          </div>
        </form>
      )}
    </Field>
  );
}
