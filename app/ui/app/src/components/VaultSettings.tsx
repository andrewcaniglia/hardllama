import { useQueryClient } from "@tanstack/react-query";
import { LockClosedIcon } from "@heroicons/react/24/outline";
import { lockVault } from "@/api";
import { Button } from "@/components/ui/button";
import { Description, Field, Label } from "@/components/ui/fieldset";

export function VaultSettings() {
  const queryClient = useQueryClient();

  const handleLock = async () => {
    queryClient.setQueryData(["vault"], await lockVault());
  };

  return (
    <Field>
      <div className="flex items-start justify-between gap-4">
        <div className="flex items-start space-x-3 flex-1">
          <LockClosedIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
          <div>
            <Label>Chat encryption</Label>
            <Description>
              Chats are encrypted on disk. They lock when you quit, lock your
              screen, or after 15 minutes idle.
            </Description>
          </div>
        </div>
        <div className="flex-shrink-0">
          <Button type="button" onClick={handleLock}>
            Lock now
          </Button>
        </div>
      </div>
    </Field>
  );
}
