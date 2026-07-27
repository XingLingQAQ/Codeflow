import { useNavigate } from 'react-router-dom';
import { Command } from 'cmdk';
import { Search, CornerDownLeft, FolderPlus, Workflow, TerminalSquare } from 'lucide-react';
import { Dialog, DialogContent } from '../ui/Dialog';
import { Kbd } from '../ui/Kbd';
import { NAV_ITEMS } from './nav';
import { STAGES } from '../stages/stageMeta';
import { useShellStore } from '../stores/shell';
import { useLayoutStore } from '../stores/layout';

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
  const lastProjectId = useLayoutStore((s) => s.lastProjectId);

  const run = (fn: () => void) => {
    setOpen(false);
    requestAnimationFrame(fn);
  };

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
                  <PaletteItem key={item.key} onSelect={() => run(() => navigate(item.path))}>
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

            <Command.Group heading="操作">
              <PaletteItem value="create new project 新建项目" onSelect={() => run(() => navigate('/projects?new=1'))}>
                <FolderPlus size={15} className="shrink-0 text-ink-dim" />
                <span>新建项目</span>
              </PaletteItem>
              <PaletteItem value="create flow 新建工作流" onSelect={() => run(() => navigate('/flows?new=1'))}>
                <Workflow size={15} className="shrink-0 text-ink-dim" />
                <span>发起新工作流</span>
              </PaletteItem>
              <PaletteItem value="legacy console 旧版控制台" onSelect={() => run(switchToLegacy)}>
                <TerminalSquare size={15} className="shrink-0 text-ink-dim" />
                <span>切换到旧版控制台</span>
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

function PaletteItem({
  children,
  onSelect,
  value,
}: {
  children: React.ReactNode;
  onSelect: () => void;
  value?: string;
}) {
  return (
    <Command.Item
      value={value}
      onSelect={onSelect}
      className="flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 pl-3 text-[13px] text-ink-dim outline-none data-[selected=true]:bg-tint-active data-[selected=true]:text-ink"
    >
      {children}
    </Command.Item>
  );
}
