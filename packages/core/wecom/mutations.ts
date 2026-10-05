import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { UpdateWecomGuestAccessRequest } from "../types";
import { wecomKeys } from "./queries";

export function useUpdateWecomGuestAccess(wsId: string, installationId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (body: UpdateWecomGuestAccessRequest) =>
      api.updateWecomGuestAccess(wsId, installationId, body),
    onSuccess: async () => {
      await Promise.all([
        qc.invalidateQueries({ queryKey: wecomKeys.installations(wsId) }),
        qc.invalidateQueries({ queryKey: wecomKeys.guestAccess(wsId, installationId) }),
      ]);
    },
  });
}
