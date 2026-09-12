/**
 * 设置可用性 view-model（E-13 契约层）。
 *
 * 规则：一个设置开关只有在存在真实功能消费者（store、后端配置或已接线的
 * 功能判断）时才可编辑；不具备能力的开关必须呈现为 unavailable 且不可编辑，
 * 并携带面向用户的理由，不允许提供可切换但不改变任何行为的假开关。
 *
 * 消费者证据（全仓 grep，2026-09-11）：autoSnapshot/livePreview 仅出现在
 * Settings.tsx 自身的 useState 与 Switch 绑定（行 33/34/143/148），无 store、
 * 后端配置或功能消费；主题/动效则经 shell store 持久化并应用到文档类，是真功能。
 */

export type SettingAvailabilityState = 'available' | 'unavailable';

export interface SettingCapabilityDeclaration {
  /** 是否存在真实功能消费者（store / 后端配置 / 已接线的功能判断） */
  hasConsumer: boolean;
  /** hasConsumer=false 时必填：向用户呈现的不可用理由 */
  unavailableReason?: string;
}

export interface SettingAvailability {
  id: string;
  state: SettingAvailabilityState;
  /** false 时控件必须禁用，不允许切换 */
  editable: boolean;
  /** state=unavailable 时的理由文案；available 时为 null */
  reason: string | null;
}

export function resolveSettingAvailability(
  id: string,
  declaration: SettingCapabilityDeclaration,
): SettingAvailability {
  if (declaration.hasConsumer) {
    return { id, state: 'available', editable: true, reason: null };
  }
  const reason = declaration.unavailableReason?.trim();
  if (!reason) {
    throw new Error(`setting "${id}" has no functional consumer and must declare unavailableReason`);
  }
  return { id, state: 'unavailable', editable: false, reason };
}

/** 当前设置页实验性开关的能力声明（E-13：两者均无功能消费者） */
export type ExperimentalSettingId = 'autoSnapshot' | 'livePreview';

export const experimentalSettingCapabilities: Record<ExperimentalSettingId, SettingCapabilityDeclaration> = {
  autoSnapshot: {
    hasConsumer: false,
    unavailableReason: '阶段自动快照尚未接入功能消费者，当前开关不控制任何行为',
  },
  livePreview: {
    hasConsumer: false,
    unavailableReason: 'Live Preview 检查器桥为 M6 预览能力，尚未交付',
  },
};

export function buildExperimentalSettingsAvailability(): SettingAvailability[] {
  return (Object.keys(experimentalSettingCapabilities) as ExperimentalSettingId[]).map((id) =>
    resolveSettingAvailability(id, experimentalSettingCapabilities[id]),
  );
}
