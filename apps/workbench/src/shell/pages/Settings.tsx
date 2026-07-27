import { useState } from 'react';
import { motion } from 'motion/react';
import { Palette, Keyboard, ShieldCheck, FlaskConical, TerminalSquare, Sun, Moon, Monitor } from 'lucide-react';
import { PageShell, SectionTitle } from './PageShell';
import { Card, CardBody, Switch, Kbd, Button, Badge } from '../../ui';
import { staggerItem } from '../../lib/motion';
import { useShellStore, type ThemeMode } from '../../stores/shell';

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
        <div className="mt-0.5 text-[12px] text-ink-dim">{desc}</div>
      </div>
      {control}
    </div>
  );
}

export default function Settings() {
  const [reducedMotion, setReducedMotion] = useState(false);
  const [autoSnapshot, setAutoSnapshot] = useState(true);
  const [livePreview, setLivePreview] = useState(false);
  const themeMode = useShellStore((s) => s.themeMode);
  const setThemeMode = useShellStore((s) => s.setThemeMode);

  const shortcuts: { keys: string[]; label: string }[] = [
    { keys: ['⌘', 'K'], label: '命令面板' },
    { keys: ['⌘', 'B'], label: '折叠 Flow Rail' },
    { keys: ['⌘', 'J'], label: '底部面板' },
    { keys: ['⌘', '\\'], label: 'Agent 伴侣' },
  ];

  return (
    <PageShell title="设置" subtitle="外观、快捷键、隐私审计与实验性开关" maxWidth="max-w-3xl">
      <motion.div variants={staggerItem} className="space-y-8">
        <section>
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
                desc="降低流程动画与过渡强度，尊重系统的 reduced-motion 设置"
                control={<Switch checked={reducedMotion} onCheckedChange={setReducedMotion} />}
              />
            </CardBody>
          </Card>
        </section>

        <section>
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

        <section>
          <SectionTitle>
            <span className="flex items-center gap-2">
              <FlaskConical size={14} /> 实验性开关
            </span>
          </SectionTitle>
          <Card>
            <CardBody className="py-1">
              <Row
                title="阶段自动快照"
                desc="每个阶段完成时创建原子快照，便于回环恢复"
                control={<Switch checked={autoSnapshot} onCheckedChange={setAutoSnapshot} />}
              />
              <Row
                title="Live Preview 检查器桥"
                desc="圈选反馈与编辑闭环（M6 预览）"
                control={<Switch checked={livePreview} onCheckedChange={setLivePreview} />}
              />
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
