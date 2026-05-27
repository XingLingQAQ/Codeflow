import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
export const ModelSearch = ({ value, onChange, placeholder = 'Search models...', className, }) => {
    return (_jsx("div", { className: className, style: {
            padding: '8px 12px',
            borderBottom: '1px solid #eee',
        }, children: _jsxs("div", { style: {
                display: 'flex',
                alignItems: 'center',
                backgroundColor: '#f5f5f5',
                borderRadius: 6,
                padding: '6px 10px',
            }, children: [_jsx("span", { style: { color: '#999', marginRight: 8 }, children: "\uD83D\uDD0D" }), _jsx("input", { type: "text", value: value, onChange: e => onChange(e.target.value), placeholder: placeholder, style: {
                        flex: 1,
                        border: 'none',
                        backgroundColor: 'transparent',
                        fontSize: 13,
                        outline: 'none',
                        color: '#333',
                    } }), value && (_jsx("button", { onClick: () => onChange(''), style: {
                        border: 'none',
                        backgroundColor: 'transparent',
                        cursor: 'pointer',
                        color: '#999',
                        fontSize: 14,
                        padding: 0,
                        marginLeft: 4,
                    }, children: "\u2715" }))] }) }));
};
export default ModelSearch;
//# sourceMappingURL=ModelSearch.js.map