function requireEditor(context) {
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
    async preview(input, context) {
        return requireEditor(context).preview(input.file, input.instruction);
    }
    async edit(input, context) {
        return requireEditor(context).edit(input.file, input.instruction);
    }
    async editMultiple(input, context) {
        return requireEditor(context).editMultiple(input.files, input.instruction);
    }
    async applyDiff(input, context) {
        await requireEditor(context).applyDiff(input.file, input.diff);
        return { applied: true };
    }
    async undo(_input, context) {
        await requireEditor(context).undo();
        return { restored: true };
    }
}
//# sourceMappingURL=FileOperationService.js.map