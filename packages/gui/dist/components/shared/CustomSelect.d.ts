/**
 * CustomSelect - 自定义下拉选择器
 * 统一风格的下拉组件，替代原生 select
 */
import React from 'react';
export interface SelectOption {
    value: string;
    label: string;
    icon?: React.ReactNode;
    description?: string;
    disabled?: boolean;
    color?: string;
}
export interface CustomSelectProps {
    options: SelectOption[];
    value?: string;
    onChange?: (value: string) => void;
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
    renderValue?: (option: SelectOption) => React.ReactNode;
}
export declare const CustomSelect: React.FC<CustomSelectProps>;
export default CustomSelect;
//# sourceMappingURL=CustomSelect.d.ts.map