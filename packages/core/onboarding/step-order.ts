import type { OnboardingStep } from "./types";

/**
 * Canonical order of the currently active onboarding steps.
 *
 * Single source of truth for "what step comes after what" — consumed
 * by the UI progress indicator to compute `index of current_step` and
 * `total step count`. Inserting, reordering, or removing a step only
 * requires changing this array; every call site that reads it updates
 * automatically.
 *
 * The fuller onboarding flow is intentionally hidden for now; first-run
 * setup only asks the user to create or choose a workspace.
 */
export const ONBOARDING_STEP_ORDER: readonly OnboardingStep[] = [
  "workspace",
] as const;
