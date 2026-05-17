import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";

export interface CASAuthConfig {
  enabled: boolean;
  displayName: string;
  loginUrl: string;
}

interface ConfigState {
  cdnDomain: string;
  allowSignup: boolean;
  googleClientId: string;
  emailLoginEnabled: boolean;
  googleLoginEnabled: boolean;
  invitationEmailEnabled: boolean;
  autoAcceptInvitationsOnLogin: boolean;
  cas: CASAuthConfig | null;
  setCdnDomain: (domain: string) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    emailLoginEnabled?: boolean;
    googleLoginEnabled?: boolean;
    invitationEmailEnabled?: boolean;
    autoAcceptInvitationsOnLogin?: boolean;
    cas?: CASAuthConfig | null;
  }) => void;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  allowSignup: true,
  googleClientId: "",
  emailLoginEnabled: true,
  googleLoginEnabled: false,
  invitationEmailEnabled: true,
  autoAcceptInvitationsOnLogin: false,
  cas: null,
  setCdnDomain: (domain) => set({ cdnDomain: domain }),
  setAuthConfig: ({
    allowSignup,
    googleClientId = "",
    emailLoginEnabled = true,
    googleLoginEnabled = Boolean(googleClientId),
    invitationEmailEnabled = true,
    autoAcceptInvitationsOnLogin = false,
    cas = null,
  }) =>
    set({
      allowSignup,
      googleClientId,
      emailLoginEnabled,
      googleLoginEnabled,
      invitationEmailEnabled,
      autoAcceptInvitationsOnLogin,
      cas,
    }),
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}
