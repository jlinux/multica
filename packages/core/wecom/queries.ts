import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Query key namespace for everything wecom-installation-related. Realtime sync
 * invalidates `installations(wsId)` on `wecom_installation:*` events so the
 * Settings panel updates without a manual refetch when another tab / an admin
 * on another machine connects or disconnects a bot.
 */
export const wecomKeys = {
  all: (wsId: string) => ["wecom", wsId] as const,
  guestAccess: (wsId: string, installationId: string) => [...wecomKeys.all(wsId), "guest-access", installationId] as const,
  installations: (wsId: string) => [...wecomKeys.all(wsId), "installations"] as const,
};

export const wecomInstallationsOptions = (wsId: string) =>
  queryOptions({
    queryKey: wecomKeys.installations(wsId),
    queryFn: () => api.listWecomInstallations(wsId),
    enabled: !!wsId,
  });

export const wecomGuestAccessOptions = (wsId: string, installationId: string) =>
  queryOptions({
    queryKey: wecomKeys.guestAccess(wsId, installationId),
    queryFn: () => api.getWecomGuestAccess(wsId, installationId),
    enabled: !!wsId && !!installationId,
    refetchOnMount: "always",
    retry: false,
  });
