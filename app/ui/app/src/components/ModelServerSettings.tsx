import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { LinkIcon } from "@heroicons/react/24/outline";
import {
  getModelServer,
  pairModelServer,
  unpairModelServer,
  type ModelServer,
} from "@/api";
import { Button } from "@/components/ui/button";
import { Description, Field, Label } from "@/components/ui/fieldset";
import { Input } from "@/components/ui/input";

export function ModelServerSettings() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["modelServer"],
    queryFn: getModelServer,
  });
  const paired = data?.address ?? "";
  const [address, setAddress] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  const update = async (action: () => Promise<ModelServer>) => {
    setIsSaving(true);
    setError("");
    try {
      queryClient.setQueryData(["modelServer"], await action());
      setAddress("");
      setCode("");
      // The model list now comes from a different computer.
      queryClient.invalidateQueries({ queryKey: ["models"] });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Something went wrong");
    } finally {
      setIsSaving(false);
    }
  };

  const handlePair = (e: FormEvent) => {
    e.preventDefault();
    void update(() => pairModelServer(address, code));
  };

  return (
    <Field>
      <div className="flex items-start space-x-3">
        <LinkIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
        <div className="w-full">
          <Label>Run models on another computer</Label>
          {paired ? (
            <>
              <Description>
                Models run on the computer at {paired}. Chats are still saved
                only on this one.
              </Description>
              <div className="mt-2">
                <Button
                  type="button"
                  plain
                  disabled={isSaving}
                  onClick={() => void update(unpairModelServer)}
                >
                  Use this computer
                </Button>
              </div>
            </>
          ) : (
            <>
              <Description>
                Use a faster computer running HardLlama. Turn on sharing there,
                click Pair a device, then enter its address and code here. Chats
                are still saved only on this computer.
              </Description>
              <form onSubmit={handlePair} className="mt-2 space-y-2">
                <Input
                  aria-label="Other computer's address"
                  placeholder="Address, such as 192.168.1.20"
                  value={address}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setAddress(e.target.value)
                  }
                />
                <Input
                  aria-label="Pairing code"
                  placeholder="Pairing code"
                  autoComplete="off"
                  value={code}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setCode(e.target.value)
                  }
                />
                <Button
                  type="submit"
                  disabled={isSaving || !address.trim() || !code.trim()}
                >
                  {isSaving ? "Pairing..." : "Pair"}
                </Button>
              </form>
            </>
          )}
          {error && (
            <p
              role="alert"
              className="mt-1 text-sm text-red-600 dark:text-red-400"
            >
              {error}
            </p>
          )}
        </div>
      </div>
    </Field>
  );
}
