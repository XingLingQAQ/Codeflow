import { useNavigate } from 'react-router-dom';
import { Command } from 'cmdk';
import { toast } from 'sonner';
import {
  Search,
  CornerDownLeft,
  FolderPlus,
  Workflow,
  TerminalSquare,
  Sun,
  Moon,
  Monitor,
  PanelLeft,
  PanelRight,
  PanelBottom,
  ArrowUpFromLine,
  MessageSquareX,
  Swords,
  FileCode2,
  Palette,
  Keyboard,
  FlaskConical,
} from 'lucide-react';
import { Dialog, DialogContent } from '../ui/Dialog';
import { Kbd } from '../ui/Kbd';
import { NAV_ITEMS, resolveNavPath } from './nav';
import { STAGES } from '../stages/stageMeta';
import { useShellStore } from '../stores/shell';
import { useLayoutStore } from '../stores/layout';
import { useEditorStore } from '../stores/editor';
import { useChatStore } from '../stores/chat';
import { promoteAllWorkspace } from '../services-bridge/workspace';
import { modLabel } from '../lib/platform';

function switchToLegacy() {
  try {
    localStorage.setItem('codeflow.shell', 'legacy');
  } catch {
    /* ignore */
  }
  window.location.assign(`${window.location.pathname}?shell=legacy`);
}

export function CommandPalette() {
  const navigate = useNavigate();
  const open = useShellStore((s) => s.commandOpen);
  const setOpen = useShellStore((s) => s.setCommandOpen);
  const setThemeMode = useShellStore((s) => s.setThemeMode);
  const lastProjectId = useLayoutStore((s) => s.lastProjectId);
  const lastStage = useLayoutStore((s) => s.lastStage);
  const flowRailCollapsed = useLayoutStore((s) => s.flowRailCollapsed);
  const companionCollapsed = useLayoutStore((s) => s.companionCollapsed);
  const bottomCollapsed = useLayoutStore((s) => s.bottomCollapsed);
  const toggleFlowRail = useLayoutStore((s) => s.toggleFlowRail);
  const toggleCompanion = useLayoutStore((s) => s.toggleCompanion);
  const toggleBottom = useLayoutStore((s) => s.toggleBottom);

  const root = useEditorStore((s) => (lastProjectId ? s.rootByProject[lastProjectId] : undefined));
  const recentTabs = useEditorStore((s) => (lastProjectId ? s.tabsByProject[lastProjectId] : undefined));
  const openFile = useEditorStore((s) => s.openFile);
  const hasMessages = useChatStore(
    (s) => !!lastProjectId && (s.messagesByProject[lastProjectId]?.length ?? 0) > 0,
  );
  const clearProject = useChatStore((s) => s.clearProject);

  const run = (fn: () => void) => {
    setOpen(false);
    requestAnimationFrame(fn);
  };

  const promoteAll = async () => {
    if (!root) return;
    try {
      const res = await promoteAllWorkspace(root);
      if (res.error) toast.warning(`部分应用完成（${res.total} 个），存在拦截：${res.error}`);
      else toast.success(`已应用全部暂存（${res.total} 个文件）`);
    } catch (err) {
      toast.error(`全部应用失败：${err instanceof Error ? err.message : String(err)}`);
    }
  };

  const mod = modLabel();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        showClose={false}
        aria-label="命令面板"
        className="top-[14%] w-[min(92vw,600px)] translate-y-0 overflow-hidden p-0"
      >
        <Command
          className="[&_[cmdk-group-heading]]:pl-11 [&_[cmdk-group-heading]]:pr-3 [&_[cmdk-group-heading]]:pb-1.5 [&_[cmdk-group-heading]]:pt-2.5 [&_[cmdk-group-heading]]:text-[11px] [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:uppercase [&_[cmdk-group-heading]]:tracking-wide [&_[cmdk-group-heading]]:text-ink-mute"
          loop
        >
          <div className="flex h-12 items-center gap-3 border-b border-line px-3">
            <Search size={16} className="shrink-0 text-ink-mute" />
            <Command.Input
              autoFocus
              placeholder="搜索命令、页面、阶段…"
              className="min-w-0 flex-1 bg-transparent text-sm leading-5 text-ink outline-none placeholder:text-ink-mute"
            />
            <Kbd>Esc</Kbd>
          </div>
          <Command.List className="max-h-[min(52vh,420px)] overflow-y-auto px-1.5 py-1.5">
            <Command.Empty className="py-8 text-center text-[13px] text-ink-mute">无匹配结果</Command.Empty>

            <Command.Group heading="页面">
              {NAV_ITEMS.map((item) => {
                const Icon = item.icon;
                return (
                  <PaletteItem
                    key={item.key}
                    onSelect={() =>
                      run(() => navigate(resolveNavPath(item, { projectId: lastProjectId, stage: lastStage })))
                    }
                  >
                    <Icon size={15} className="shrink-0 text-ink-dim" />
                    <span>{item.label}</span>
                  </PaletteItem>
                );
              })}
            </Command.Group>

            {lastProjectId && (
              <Command.Group heading="跳转到阶段">
                {STAGES.map((stage) => {
                  const Icon = stage.icon;
                  return (
                    <PaletteItem
                      key={stage.type}
                      value={`stage ${stage.en} ${stage.label}`}
                      onSelect={() => run(() => navigate(`/workbench/${lastProjectId}/${stage.type}`))}
                    >
                      <Icon size={15} className="shrink-0 text-ink-dim" />
                      <span>
                        {stage.index}. {stage.label} 画布
                      </span>
                    </PaletteItem>
                  );
                })}
              </Command.Group>
            )}

            {recentTabs && recentTabs.length > 0 && (
              <Command.Group heading="最近文件">
                {recentTabs.slice(-5).reverse().map((t) => (
                  <PaletteItem
                    key={t.path}
                    value={`recent file ${t.path}`}
                    onSelect={() =>
                      run(() => {
                        if (!lastProjectId) return;
                        openFile(lastProjectId, t.path);
                        navigate(`/workbench/${lastProjectId}/coding`);
                      })
                    }
                  >
                    <FileCode2 size={15} className="shrink-0 text-ink-dim" />
                    <span className="truncate font-mono text-[12px]">{t.path}</span>
                  </PaletteItem>
                ))}
              </Command.Group>
            )}

            <Command.Group heading="面板">
              <PaletteItem
                value="panel flow rail 阶段栏 折叠 展开"
                onSelect={() => run(toggleFlowRail)}
                hint={<KbdPair mod={mod} k="B" />}
              >
                <PanelLeft size={15} className="shrink-0 text-ink-dim" />
                <span>{flowRailCollapsed ? '展开阶段栏' : '收起阶段栏'}</span>
              </PaletteItem>
              <PaletteItem
                value="panel bottom 底部面板 折叠 展开"
                onSelect={() => run(toggleBottom)}
                hint={<KbdPair mod={mod} k="J" />}
              >
                <PanelBottom size={15} className="shrink-0 text-ink-dim" />
                <span>{bottomCollapsed ? '展开底部面板' : '收起底部面板'}</span>
              </PaletteItem>
              <PaletteItem
                value="panel companion agent 伴侣 折叠 展开"
                onSelect={() => run(toggleCompanion)}
                hint={<KbdPair mod={mod} k="\" />}
              >
                <PanelRight size={15} className="shrink-0 text-ink-dim" />
                <span>{companionCollapsed ? '展开 Agent 伴侣' : '收起 Agent 伴侣'}</span>
              </PaletteItem>
            </Command.Group>

            <Command.Group heading="主题">
              <PaletteItem value="theme light 浅色主题" onSelect={() => run(() => setThemeMode('light'))}>
                <Sun size={15} className="shrink-0 text-ink-dim" />
                <span>切换到浅色主题</span>
              </PaletteItem>
              <PaletteItem value="theme dark 深色主题" onSelect={() => run(() => setThemeMode('dark'))}>
                <Moon size={15} className="shrink-0 text-ink-dim" />
                <span>切换到深色主题</span>
              </PaletteItem>
              <PaletteItem value="theme system 跟随系统主题" onSelect={() => run(() => setThemeMode('system'))}>
                <Monitor size={15} className="shrink-0 text-ink-dim" />
                <span>主题跟随系统</span>
              </PaletteItem>
            </Command.Group>

            <Command.Group heading="操作">
              <PaletteItem value="create new project 新建项目" onSelect={() => run(() => navigate('/projects?new=1'))}>
                <FolderPlus size={15} className="shrink-0 text-ink-dim" />
                <span>新建项目</span>
              </PaletteItem>
              <PaletteItem value="create flow 新建工作流" onSelect={() => run(() => navigate('/flows?new=1'))}>
                <Workflow size={15} className="shrink-0 text-ink-dim" />
                <span>发起新工作流</span>
              </PaletteItem>
              {root && (
                <PaletteItem value="promote all staged 应用全部暂存" onSelect={() => run(promoteAll)}>
                  <ArrowUpFromLine size={15} className="shrink-0 text-ink-dim" />
                  <span>应用全部暂存到工作树</span>
                </PaletteItem>
              )}
              {hasMessages && (
                <PaletteItem
                  value="clear conversation 清空当前对话"
                  onSelect={() =>
                    run(() => {
                      if (lastProjectId) {
                        clearProject(lastProjectId);
                        toast.success('已清空当前项目对话');
                      }
                    })
                  }
                >
                  <MessageSquareX size={15} className="shrink-0 text-ink-dim" />
                  <span>清空当前对话</span>
                </PaletteItem>
              )}
              <PaletteItem
                value="start debate 发起辩论"
                onSelect={() => run(() => toast('多方辩论将在 M4 交付，敬请期待', { duration: 3500 }))}
              >
                <Swords size={15} className="shrink-0 text-ink-dim" />
                <span>发起辩论</span>
              </PaletteItem>
              <PaletteItem value="legacy console 旧版控制台" onSelect={() => run(switchToLegacy)}>
                <TerminalSquare size={15} className="shrink-0 text-ink-dim" />
                <span>切换到旧版控制台</span>
              </PaletteItem>
            </Command.Group>

            <Command.Group heading="设置">
              <PaletteItem value="settings appearance 设置 外观 主题" onSelect={() => run(() => navigate('/settings#appearance'))}>
                <Palette size={15} className="shrink-0 text-ink-dim" />
                <span>设置 · 外观</span>
              </PaletteItem>
              <PaletteItem value="settings shortcuts 设置 快捷键" onSelect={() => run(() => navigate('/settings#shortcuts'))}>
                <Keyboard size={15} className="shrink-0 text-ink-dim" />
                <span>设置 · 快捷键</span>
              </PaletteItem>
              <PaletteItem value="settings experimental 设置 实验开关" onSelect={() => run(() => navigate('/settings#experimental'))}>
                <FlaskConical size={15} className="shrink-0 text-ink-dim" />
                <span>设置 · 实验性开关</span>
              </PaletteItem>
            </Command.Group>
          </Command.List>
          <div className="flex items-center justify-between border-t border-line px-3 py-2 text-[11px] text-ink-mute">
            <span className="flex items-center gap-1.5">
              <CornerDownLeft size={12} /> 选择
            </span>
            <span>CodeFlow</span>
          </div>
        </Command>
      </DialogContent>
    </Dialog>
  );
}

function KbdPair({ mod, k }: { mod: string; k: string }) {
  return (
    <span className="ml-auto flex items-center gap-1">
      <Kbd>{mod}</Kbd>
      <Kbd>{k}</Kbd>
    </span>
  );
}

function PaletteItem({
  children,
  onSelect,
  value,
  hint,
}: {
  children: React.ReactNode;
  onSelect: () => void;
  value?: string;
  hint?: React.ReactNode;
}) {
  return (
    <Command.Item
      value={value}
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 pl-3 text-[13px] text-ink-dim outline-none data-[selected=true]:bg-tint-active data-[selected=true]:text-ink"
    >
      {children}
      {hint}
    </Command.Item>
  );
}
