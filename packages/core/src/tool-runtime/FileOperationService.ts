import type { Diff, EditResult, ICodeEditor } from '../cowork/types.js';
import type {
  FileApplyDiffInput,
  FileEditInput,
  FileEditMultipleInput,
  FilePreviewInput,
  FileUndoResult,
  ToolContext,
} from './types.js';

function requireEditor(context: ToolContext): ICodeEditor {
  const editor = context.resources?.editor;
  if (!editor) {
    throw new Error('FileOperationService requires context.resources.editor');
  }
  return editor;
}

/**
 * FileOperationService
 * 统一承接 preview/edit/applyDiff/undo 文件工具能力
 */
export class FileOperationService {
  async preview(input: FilePreviewInput, context: ToolContext): Promise<Diff> {
    return requireEditor(context).preview(input.file, input.instruction);
  }

  async edit(input: FileEditInput, context: ToolContext): Promise<EditResult> {
    return requireEditor(context).edit(input.file, input.instruction);
  }

  async editMultiple(input: FileEditMultipleInput, context: ToolContext): Promise<EditResult[]> {
    return requireEditor(context).editMultiple(input.files, input.instruction);
  }

  async applyDiff(input: FileApplyDiffInput, context: ToolContext): Promise<{ applied: true }> {
    await requireEditor(context).applyDiff(input.file, input.diff);
    return { applied: true };
  }

  async undo(_input: Record<string, never>, context: ToolContext): Promise<FileUndoResult> {
    await requireEditor(context).undo();
    return { restored: true };
  }
}
