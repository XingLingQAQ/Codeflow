import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { MemoryRouter } from 'react-router-dom';
import Settings from './Settings';
import { useShellStore } from '../../stores/shell';

// React 19 的 act 需要显式声明 act 环境
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

let container: HTMLDivElement;
let root: Root;

function renderSettings() {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
  act(() => {
    root.render(
      <MemoryRouter>
        <Settings />
      </MemoryRouter>,
    );
  });
}

function click(el: Element) {
  act(() => {
    el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
  });
}

// Row 的标题是叶子 div；父级 div 的 textContent 还会拼接描述/理由，不会精确相等
function findRowByTitle(title: string): HTMLElement {
  const leaf = Array.from(container.querySelectorAll('div')).find((d) => d.textContent === title);
  if (!leaf) throw new Error(`row title not found: ${title}`);
  const row = leaf.parentElement?.parentElement;
  if (!row) throw new Error(`row container not found for: ${title}`);
  return row;
}

function switchInRow(title: string): HTMLButtonElement {
  const sw = findRowByTitle(title).querySelector('button[role="switch"]');
  if (!sw) throw new Error(`switch not found in row: ${title}`);
  return sw as HTMLButtonElement;
}

function localStorageKeys(): string[] {
  const keys: string[] = [];
  for (let i = 0; i < localStorage.length; i++) keys.push(localStorage.key(i) ?? '');
  return keys;
}

beforeAll(() => {
  // jsdom 未实现 matchMedia；主题/动效的 system 档解析依赖它，补最小桩
  if (typeof window.matchMedia !== 'function') {
    window.matchMedia = ((query: string): MediaQueryList => ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })) as typeof window.matchMedia;
  }
});

afterEach(() => {
  act(() => root.unmount());
  container.remove();
  // 恢复真功能默认值，避免文件内用例串扰
  useShellStore.getState().setThemeMode('light');
  useShellStore.getState().setMotionPref('system');
  localStorage.clear();
});

// §28 T13.05.c 点名 FU（组件层）：由 b 步 describe 重命名合并而来，断言逐字保留。
// 模型层同名 describe 在 settingsAvailability.test.ts（view-model 裁决与 store setter）。
describe('UnavailableExperimentalSettingsCannotToggle（E-13：无消费者的开关只能呈现 unavailable）', () => {
  it('两个假开关渲染为禁用态且理由可见', () => {
    renderSettings();

    const autoSnapshot = switchInRow('阶段自动快照');
    const livePreview = switchInRow('Live Preview 检查器桥');
    expect(autoSnapshot.disabled).toBe(true);
    expect(livePreview.disabled).toBe(true);
    expect(autoSnapshot.getAttribute('data-state')).toBe('unchecked');
    expect(livePreview.getAttribute('data-state')).toBe('unchecked');

    expect(container.textContent).toContain('尚未接入功能消费者，当前开关不控制任何行为');
    expect(container.textContent).toContain('Live Preview 检查器桥为 M6 预览能力，尚未交付');
  });

  it('禁用开关点击不改变状态，也不做任何本地持久化', () => {
    renderSettings();

    click(switchInRow('阶段自动快照'));
    click(switchInRow('Live Preview 检查器桥'));

    expect(switchInRow('阶段自动快照').getAttribute('data-state')).toBe('unchecked');
    expect(switchInRow('Live Preview 检查器桥').getAttribute('data-state')).toBe('unchecked');
    expect(localStorageKeys().filter((k) => /snapshot|livepreview/i.test(k))).toEqual([]);
  });
});

describe('ThemeAndMotionSettingsStillPersist（主题/动效真功能不受假开关处置影响）', () => {
  it('主题与减少动效仍可切换并持久化', () => {
    renderSettings();

    const darkButton = Array.from(container.querySelectorAll('button')).find((b) =>
      b.textContent?.includes('深色'),
    );
    expect(darkButton).toBeTruthy();
    click(darkButton!);
    expect(useShellStore.getState().themeMode).toBe('dark');
    expect(localStorage.getItem('codeflow.theme')).toBe('dark');
    expect(document.documentElement.classList.contains('dark')).toBe(true);

    const motionSwitch = switchInRow('减少动效');
    expect(motionSwitch.disabled).toBe(false);
    click(motionSwitch);
    expect(useShellStore.getState().motionPref).toBe('reduce');
    expect(localStorage.getItem('codeflow.motion')).toBe('reduce');
    expect(document.documentElement.classList.contains('cf-reduced')).toBe(true);
  });
});
