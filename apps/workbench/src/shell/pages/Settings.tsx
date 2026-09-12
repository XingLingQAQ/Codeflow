import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { motion } from 'motion/react';
import { Palette, Keyboard, ShieldCheck, FlaskConical, TerminalSquare, Sun, Moon, Monitor } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, CardBody, Switch, Kbd, Button } from '../../ui';
import { staggerItem } from '../../lib/motion';
import { modLabel } from '../../lib/platform';
import { useShellStore, type ThemeMode } from '../../stores/shell';
import {
  buildExperimentalSettingsAvailability,
  type ExperimentalSettingId,
} from './settingsAvailability';

// 实验性开关的静态展示信息；可用性（可否编辑、禁用理由）由 settingsAvailability
// view-model 裁决，见下方 experimental 区块注释。
const experimentalMeta: Record<ExperimentalSettingId, { title: string; desc: string }> = {
  autoSnapshot: { title: '阶段自动快照', desc: '每个阶段完成时创建原子快照，便于回环恢复' },
  livePreview: { title: 'Live Preview 检查器桥', desc: '圈选反馈与编辑闭环（M6 预览）' },
};

function switchToLegacy() {
  try {
    localStorage.setItem('codeflow.shell', 'legacy');
  } catch {
    /* ignore */
  }
  window.location.assign(`${window.location.pathname}?shell=legacy`);
}

function Row({ title, desc, control }: { title: string; desc: string; control: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-line py-3.5 last:border-0">
      <div>
        <div className="text-[13px] font-medium text-ink">{title}</div>
        <div className="mt-0.5 text-[12px] leading-relaxed text-ink-dim">{desc}</div>
      </div>
      {control}
    </div>
  );
}

export default function Settings() {
  const themeMode = useShellStore((s) => s.themeMode);
  const setThemeMode = useShellStore((s) => s.setThemeMode);
  const motionPref = useShellStore((s) => s.motionPref);
  const setMotionPref = useShellStore((s) => s.setMotionPref);
  const { hash } = useLocation();

  // E-13：实验性开关不做本地假状态。可用性由 view-model 裁决——当前
  // autoSnapshot/livePreview 均无功能消费者，只能呈现为 unavailable 禁用态并
  // 展示理由，不做任何本地持久化。将来真实能力/配置/消费者齐全时，先在
  // settingsAvailability.ts 的 experimentalSettingCapabilities 中声明 hasConsumer
  // 并接入真实读写，此处才会变为可编辑（settingsAvailability.test.ts 钉住当前
  // 两个开关均为 unavailable，声明变更会被测试拦住，倒逼接线而不是复活假开关）。
  const experimentalSettings = buildExperimentalSettingsAvailability();

  // Palette section jumps land here as /settings#appearance etc.
  useEffect(() => {
    if (!hash) return;
    const el = document.getElementById(hash.slice(1));
    if (el) el.scrollIntoView({ block: 'start' });
  }, [hash]);

  const mod = modLabel();
  const shortcuts: { keys: string[]; label: string }[] = [
    { keys: [mod, 'K'], label: '命令面板' },
    { keys: [mod, 'B'], label: '折叠 / 展开阶段栏' },
    { keys: [mod, 'J'], label: '折叠 / 展开底部面板' },
    { keys: [mod, '\\'], label: '折叠 / 展开 Agent 伴侣' },
  ];

  return (
    <PageShell title="设置" subtitle="外观、快捷键、隐私审计与实验性开关" maxWidth="max-w-3xl">
      <motion.div variants={staggerItem} className="space-y-8">
        <section id="appearance">
          <SectionTitle>
            <span className="flex items-center gap-2">
              <Palette size={14} /> 外观
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="py-1">
              <Row
                title="主题"
                desc="选择浅色、深色或跟随系统偏好"
                control={
                  <div className="flex gap-1">
                    {([
                      { mode: 'light' as ThemeMode, icon: Sun, label: '浅色' },
                      { mode: 'dark' as ThemeMode, icon: Moon, label: '深色' },
                      { mode: 'system' as ThemeMode, icon: Monitor, label: '系统' },
                    ]).map(({ mode, icon: Icon, label }) => (
                      <button
                        key={mode}
                        onClick={() => setThemeMode(mode)}
                        className={`flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-[12px] font-medium transition-colors ${
                          themeMode === mode
                            ? '[background:var(--btn-primary-bg)] [color:var(--btn-primary-fg)] border border-transparent'
                            : 'text-ink-dim hover:text-ink border border-transparent'
                        }`}
                      >
                        <Icon size={13} /> {label}
                      </button>
                    ))}
                  </div>
                }
              />
              <Row
                title="减少动效"
                desc="将界面过渡与流程动画降级为瞬时切换；「跟随系统」时遵循操作系统的 reduced-motion 设置"
                control={
                  <Switch
                    checked={motionPref === 'reduce'}
                    onCheckedChange={(on) => setMotionPref(on ? 'reduce' : 'system')}
                  />
                }
              />
            </CardBody>
          </Card>
        </section>

        <section id="shortcuts">
          <SectionTitle>
            <span className="flex items-center gap-2">
              <Keyboard size={14} /> 快捷键
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="py-1">
              {shortcuts.map((s) => (
                <Row
                  key={s.label}
                  title={s.label}
                  desc="全局快捷键"
                  control={
                    <span className="flex items-center gap-1">
                      {s.keys.map((k) => (
                        <Kbd key={k}>{k}</Kbd>
                      ))}
                    </span>
                  }
                />
              ))}
            </CardBody>
          </Card>
        </section>

        <section id="experimental">
          <SectionTitle>
            <span className="flex items-center gap-2">
              <FlaskConical size={14} /> 实验性开关
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="py-1">
              {experimentalSettings.map((setting) => {
                const meta = experimentalMeta[setting.id as ExperimentalSettingId];
                return (
                  <Row
                    key={setting.id}
                    title={meta.title}
                    desc={setting.reason ? `${meta.desc}。${setting.reason}` : meta.desc}
                    control={
                      <Switch
                        checked={false}
                        disabled={!setting.editable}
                        aria-label={meta.title}
                      />
                    }
                  />
                );
              })}
            </CardBody>
          </Card>
        </section>

        <section>
          <SectionTitle>
            <span className="flex items-center gap-2">
              <ShieldCheck size={14} /> 隐私与审计
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="text-[13px] leading-relaxed text-ink-dim">
              所有写操作经守卫拦截并写入审计日志；隐私披露预检在上下文注入前运行。完整的隐私审计面板将随配置中心（M5）上线。
            </CardBody>
          </Card>
        </section>

        <section>
          <SectionTitle>
            <span className="flex items-center gap-2">
              <TerminalSquare size={14} /> 旧版控制台
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="flex items-center justify-between gap-4">
              <p className="text-[13px] leading-relaxed text-ink-dim">
                新 Shell 已是默认产品界面。如需回到旧版会话/计划控制台，可临时切换。
              </p>
              <Button variant="secondary" onClick={switchToLegacy}>
                切换到旧版
              </Button>
            </CardBody>
          </Card>
        </section>
      </motion.div>
    </PageShell>
  );
}
