// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import { wecomKeys } from "@multica/core/wecom";
import enSettings from "../../locales/en/settings.json";
import enCommon from "../../locales/en/common.json";
import zhSettings from "../../locales/zh-Hans/settings.json";
import zhCommon from "../../locales/zh-Hans/common.json";
import { WecomGuestAccessDialog } from "./wecom-guest-access-dialog";

const getPolicy = vi.hoisted(() => vi.fn());
const updatePolicy = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/core/api")>(),
  api: { getWecomGuestAccess: getPolicy, updateWecomGuestAccess: updatePolicy },
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getMemberName: (id: string) => id === "owner-1" ? "Current owner" : "Saving admin" }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn() } }));
import { toast } from "sonner";

const policy = {
  enabled: true, allowedGroupIds: ["group-a", "unrecorded-group"], allowDirectMessages: false,
  version: "environment:v1", source: "environment", sponsorUserId: "owner-1", updatedBy: null, updatedAt: null,
  groups: [{ chatId: "group-a", name: null }, { chatId: "group-b", name: "Known group" }],
};
const close = vi.fn();
function renderDialog(locale: "en" | "zh-Hans" = "en", qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })) {
  return render(<QueryClientProvider client={qc}>
    <I18nProvider locale={locale} resources={{ en: { settings: enSettings, common: enCommon }, "zh-Hans": { settings: zhSettings, common: zhCommon } }}>
      <WecomGuestAccessDialog wsId="ws-1" installationId="i1" onClose={close} />
    </I18nProvider>
  </QueryClientProvider>);
}
beforeEach(() => {
  vi.clearAllMocks();
  getPolicy.mockResolvedValue(policy);
  updatePolicy.mockResolvedValue({ ...policy, source: "database", version: "database:v2" });
});
afterEach(cleanup);

// Input boundary matrices live in core/wecom/guest-access.test.ts. This suite
// exercises the shared UI wiring, fresh-load guard and retained edit regressions.
describe("WeCom guest access dialog", () => {
  it("shows loading without switches or a writable empty policy", () => {
    getPolicy.mockReturnValue(new Promise(() => {}));
    renderDialog();
    expect(screen.getByRole("dialog")).toBeTruthy();
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("does not expose cached data as writable when the fresh load fails", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    qc.setQueryData(wecomKeys.guestAccess("ws-1", "i1"), policy);
    getPolicy.mockRejectedValue(new Error("Invalid WeCom guest access response"));
    renderDialog("en", qc);
    expect(await screen.findByRole("alert")).toHaveTextContent(/could not load/i);
    expect(screen.queryByRole("switch")).toBeNull();
    expect(updatePolicy).not.toHaveBeenCalled();
    getPolicy.mockResolvedValue(policy);
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("switch", { name: "Allow people without an account" })).toBeChecked();
  });

  it("loads environment grants, retains unrecorded groups, edits known/manual groups and saves private access", async () => {
    renderDialog();
    expect(await screen.findByRole("switch", { name: "Allow people without an account" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "unrecorded-group" })).toBeChecked();
    expect(screen.getByText("Current owner")).toBeTruthy();
    expect(screen.getByText(/all external identities/i)).toBeTruthy();
    expect(screen.getByText(/group members can see/i)).toBeTruthy();
    await userEvent.click(screen.getByRole("checkbox", { name: "group-a" }));
    await userEvent.click(screen.getByRole("checkbox", { name: /Known group.*group-b/ }));
    await userEvent.type(screen.getByLabelText("Group ID"), "  manual-group  ");
    await userEvent.click(screen.getByRole("button", { name: "Add group" }));
    await userEvent.click(screen.getByRole("switch", { name: "Allow guest direct messages" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(updatePolicy).toHaveBeenCalledWith("ws-1", "i1", {
      enabled: true, allowedGroupIds: ["unrecorded-group", "group-b", "manual-group"],
      allowDirectMessages: true, version: "environment:v1",
    }));
    expect(close).toHaveBeenCalled();
  });

  it("keeps known group labels attached to their checkbox after manual additions and removals", async () => {
    renderDialog();
    await screen.findByRole("switch", { name: "Allow people without an account" });
    await userEvent.type(screen.getByLabelText("Group ID"), "new-manual-group");
    await userEvent.click(screen.getByRole("button", { name: "Add group" }));
    expect(screen.getByRole("checkbox", { name: /Known group.*group-b/ })).not.toBeChecked();
    expect(screen.getByRole("checkbox", { name: "new-manual-group" })).toBeChecked();
    await userEvent.click(screen.getByText("Known group"));
    expect(screen.getByRole("checkbox", { name: /Known group.*group-b/ })).toBeChecked();
    await userEvent.click(screen.getByRole("checkbox", { name: "new-manual-group" }));
    expect(screen.getByRole("checkbox", { name: /Known group.*group-b/ })).toBeChecked();
    expect(screen.queryByRole("checkbox", { name: "new-manual-group" })).toBeNull();
  });

  it("requires at least one group or direct messages before enabling guest access", async () => {
    getPolicy.mockResolvedValue({ ...policy, enabled: false, allowedGroupIds: [], groups: [] });
    renderDialog();
    await screen.findByRole("switch", { name: "Allow people without an account" });
    await userEvent.click(screen.getByRole("switch", { name: "Allow people without an account" }));
    expect(screen.getByRole("alert")).toHaveTextContent(/allow at least one group or direct messages/i);
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    await userEvent.click(screen.getByRole("switch", { name: "Allow guest direct messages" }));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("preserves edits after a failed save without a success toast", async () => {
    updatePolicy.mockRejectedValue(new Error("Invalid WeCom guest access response"));
    renderDialog();
    await screen.findByRole("switch", { name: "Allow people without an account" });
    await userEvent.click(screen.getByRole("switch", { name: "Allow people without an account" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/could not save/i);
    expect(screen.getByRole("switch", { name: "Allow people without an account" })).not.toBeChecked();
    expect(close).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("explains conflicts, keeps edits and explicitly reloads before saving again", async () => {
    updatePolicy.mockRejectedValue(new ApiError("stale", 409, "Conflict"));
    renderDialog();
    await screen.findByRole("switch", { name: "Allow people without an account" });
    await userEvent.click(screen.getByRole("switch", { name: "Allow guest direct messages" }));
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent(/another administrator/i);
    expect(screen.getByRole("switch", { name: "Allow guest direct messages" })).toBeChecked();
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    getPolicy.mockResolvedValue({ ...policy, enabled: false, version: "database:other" });
    await userEvent.click(screen.getByRole("button", { name: "Reload latest (discard edits)" }));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Allow people without an account" })).not.toBeChecked());
    expect(screen.getByRole("button", { name: "Save" })).toBeEnabled();
  });

  it("shows read-only saved attribution and Chinese consequence copy", async () => {
    getPolicy.mockResolvedValue({ ...policy, source: "database", updatedBy: "admin-1", updatedAt: "2026-10-05T04:00:00Z" });
    renderDialog("zh-Hans");
    expect(await screen.findByRole("switch", { name: "允许无账号用户访问" })).toBeChecked();
    expect(screen.getByText("Saving admin")).toBeTruthy();
    expect(screen.getByText(/所有能够私聊/)).toBeTruthy();
    expect(screen.queryByRole("combobox")).toBeNull();
  });
});
