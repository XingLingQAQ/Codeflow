import { describe, expect, it } from 'vitest';
import {
  buildExperimentalSettingsAvailability,
  experimentalSettingCapabilities,
  resolveSettingAvailability,
} from './settingsAvailability';
import { useShellStore } from '../../stores/shell';

describe('UnavailableExperimentalSettingsCannotToggle', () => {
  it('autoSnapshot declares no consumer and resolves unavailable with a reason', () => {
    const availability = resolveSettingAvailability('autoSnapshot', experimentalSettingCapabilities.autoSnapshot);
    expect(availability).toEqual({
      id: 'autoSnapshot',
      state: 'unavailable',
      editable: false,
      reason: expect.any(String),
    });
    expect(availability.reason?.length).toBeGreaterThan(0);
  });

  it('livePreview declares no consumer and resolves unavailable with a reason', () => {
    const availability = resolveSettingAvailability('livePreview', experimentalSettingCapabilities.livePreview);
    expect(availability.state).toBe('unavailable');
    expect(availability.editable).toBe(false);
    expect(availability.reason?.length).toBeGreaterThan(0);
  });

  it('every current experimental setting is unavailable and non-editable', () => {
    const all = buildExperimentalSettingsAvailability();
    expect(all.map((a) => a.id).sort()).toEqual(['autoSnapshot', 'livePreview']);
    for (const availability of all) {
      expect(availability.state).toBe('unavailable');
      expect(availability.editable).toBe(false);
      expect(availability.reason).toBeTruthy();
    }
  });

  it('rejects an unavailable declaration without a user-facing reason', () => {
    expect(() => resolveSettingAvailability('broken', { hasConsumer: false })).toThrow(/unavailableReason/);
    expect(() => resolveSettingAvailability('broken', { hasConsumer: false, unavailableReason: '  ' })).toThrow(
      /unavailableReason/,
    );
  });
});

describe('ThemeAndMotionSettingsStillPersist', () => {
  it('a wired capability stays available and editable without a reason', () => {
    const availability = resolveSettingAvailability('themeMode', { hasConsumer: true });
    expect(availability).toEqual({ id: 'themeMode', state: 'available', editable: true, reason: null });
  });

  it('theme mode setter persists to localStorage and applies the dark class', () => {
    const { setThemeMode } = useShellStore.getState();
    setThemeMode('dark');
    expect(useShellStore.getState().themeMode).toBe('dark');
    expect(localStorage.getItem('codeflow.theme')).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);
    setThemeMode('light');
    expect(localStorage.getItem('codeflow.theme')).toBe('light');
    expect(document.documentElement.classList.contains('dark')).toBe(false);
  });

  it('motion pref setter persists to localStorage and applies the reduced class', () => {
    const { setMotionPref } = useShellStore.getState();
    setMotionPref('reduce');
    expect(useShellStore.getState().motionPref).toBe('reduce');
    expect(localStorage.getItem('codeflow.motion')).toBe('reduce');
    expect(document.documentElement.classList.contains('cf-reduced')).toBe(true);
    setMotionPref('full');
    expect(localStorage.getItem('codeflow.motion')).toBe('full');
    expect(document.documentElement.classList.contains('cf-reduced')).toBe(false);
  });
});
