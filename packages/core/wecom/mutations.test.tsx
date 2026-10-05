// @vitest-environment jsdom
import { createElement, type ReactNode } from "react";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { wecomGuestAccessOptions, wecomKeys } from "./queries";
import { useUpdateWecomGuestAccess } from "./mutations";
const update = vi.hoisted(() => vi.fn());
vi.mock("../api", () => ({ api: { updateWecomGuestAccess: update } }));

describe("WeCom guest access queries and mutation", () => {
  it("scopes policy queries by workspace and installation and requires a fresh dialog load", () => {
    const options = wecomGuestAccessOptions("ws-1", "i1");
    expect(options.queryKey).not.toEqual(wecomGuestAccessOptions("ws-2", "i1").queryKey);
    expect(options.queryKey).not.toEqual(wecomGuestAccessOptions("ws-1", "i2").queryKey);
    expect(options.refetchOnMount).toBe("always");
    expect(wecomGuestAccessOptions("", "i1").enabled).toBe(false);
  });

  it("awaits a confirmed save and refreshes scoped list and detail", async () => {
    const qc = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    update.mockResolvedValue({ version: "v2" });
    const { result } = renderHook(() => useUpdateWecomGuestAccess("ws-1", "i1"), {
      wrapper: ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client: qc }, children),
    });
    const body = { enabled: true, allowedGroupIds: ["group-a"], allowDirectMessages: false, version: "v1" };
    await act(async () => { await result.current.mutateAsync(body); });
    expect(update).toHaveBeenCalledWith("ws-1", "i1", body);
    expect(invalidate).toHaveBeenCalledWith({ queryKey: wecomKeys.installations("ws-1") });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: wecomKeys.guestAccess("ws-1", "i1") });
  });
});
