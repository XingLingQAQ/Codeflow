/**
 * CustomSelect - 自定义下拉选择器
 * 统一风格的下拉组件，替代原生 select
 * 支持分组选项、多选模式、异步加载
 */
import React from 'react';
export interface SelectOption {
    value: string;
    label: string;
    icon?: React.ReactNode;
    description?: string;
    disabled?: boolean;
    color?: string;
    group?: string;
}
export interface OptionGroup {
    label: string;
    options: SelectOption[];
}
interface CustomSelectPropsBase {
    options?: SelectOption[];
    groupedOptions?: OptionGroup[];
    placeholder?: string;
    disabled?: boolean;
    loading?: boolean;
    searchable?: boolean;
    clearable?: boolean;
    size?: 'sm' | 'md' | 'lg';
    variant?: 'default' | 'outlined' | 'filled';
    error?: boolean;
    helperText?: string;
    label?: string;
    className?: string;
    style?: React.CSSProperties;
    dropdownStyle?: React.CSSProperties;
    renderOption?: (option: SelectOption, isSelected: boolean) => React.ReactNode;
    onLoadMore?: () => void;
    hasMore?: boolean;
    loadingMore?: boolean;
}
interface SingleSelectProps extends CustomSelectPropsBase {
    multiple?: false;
    value?: string;
    onChange?: (value: string) => void;
    renderValue?: (option: SelectOption) => React.ReactNode;
    maxSelections?: never;
}
interface MultiSelectProps extends CustomSelectPropsBase {
    multiple: true;
    value?: string[];
    onChange?: (value: string[]) => void;
    renderValue?: (options: SelectOption[]) => React.ReactNode;
    maxSelections?: number;
}
export type CustomSelectProps = SingleSelectProps | MultiSelectProps;
export declare const CustomSelect: React.FC<CustomSelectProps>;
export default CustomSelect;
//# sourceMappingURL=CustomSelect.d.ts.map