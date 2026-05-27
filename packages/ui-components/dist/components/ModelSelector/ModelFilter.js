import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { PROVIDER_ICONS, PROVIDER_NAMES, CAPABILITY_NAMES, CAPABILITY_COLORS, } from './types';
export const ModelFilter = ({ providers, capabilities, selectedProvider, selectedCapabilities = [], onProviderChange, onCapabilitiesChange, className, }) => {
    const handleCapabilityToggle = (cap) => {
        if (selectedCapabilities.includes(cap)) {
            onCapabilitiesChange(selectedCapabilities.filter(c => c !== cap));
        }
        else {
            onCapabilitiesChange([...selectedCapabilities, cap]);
        }
    };
    return (_jsxs("div", { className: className, style: { padding: '8px 12px' }, children: [_jsxs("div", { style: { marginBottom: 12 }, children: [_jsx("div", { style: {
                            fontSize: 11,
                            fontWeight: 600,
                            color: '#666',
                            marginBottom: 6,
                            textTransform: 'uppercase',
                        }, children: "Provider" }), _jsxs("div", { style: { display: 'flex', flexWrap: 'wrap', gap: 4 }, children: [_jsx("button", { onClick: () => onProviderChange(undefined), style: {
                                    padding: '4px 10px',
                                    fontSize: 12,
                                    border: '1px solid #ddd',
                                    borderRadius: 4,
                                    backgroundColor: !selectedProvider ? '#2196F3' : '#fff',
                                    color: !selectedProvider ? '#fff' : '#333',
                                    cursor: 'pointer',
                                    transition: 'all 0.15s',
                                }, children: "All" }), providers.map(provider => (_jsxs("button", { onClick: () => onProviderChange(provider), style: {
                                    padding: '4px 10px',
                                    fontSize: 12,
                                    border: '1px solid #ddd',
                                    borderRadius: 4,
                                    backgroundColor: selectedProvider === provider ? '#2196F3' : '#fff',
                                    color: selectedProvider === provider ? '#fff' : '#333',
                                    cursor: 'pointer',
                                    transition: 'all 0.15s',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 4,
                                }, children: [_jsx("span", { children: PROVIDER_ICONS[provider] }), _jsx("span", { children: PROVIDER_NAMES[provider] })] }, provider)))] })] }), _jsxs("div", { children: [_jsx("div", { style: {
                            fontSize: 11,
                            fontWeight: 600,
                            color: '#666',
                            marginBottom: 6,
                            textTransform: 'uppercase',
                        }, children: "Capabilities" }), _jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: 4 }, children: capabilities.map(cap => {
                            const isSelected = selectedCapabilities.includes(cap);
                            return (_jsx("button", { onClick: () => handleCapabilityToggle(cap), style: {
                                    padding: '4px 8px',
                                    fontSize: 11,
                                    border: `1px solid ${isSelected ? CAPABILITY_COLORS[cap] : '#ddd'}`,
                                    borderRadius: 4,
                                    backgroundColor: isSelected ? `${CAPABILITY_COLORS[cap]}15` : '#fff',
                                    color: isSelected ? CAPABILITY_COLORS[cap] : '#666',
                                    cursor: 'pointer',
                                    transition: 'all 0.15s',
                                    fontWeight: isSelected ? 500 : 400,
                                }, children: CAPABILITY_NAMES[cap] }, cap));
                        }) })] }), (selectedProvider || selectedCapabilities.length > 0) && (_jsx("button", { onClick: () => {
                    onProviderChange(undefined);
                    onCapabilitiesChange([]);
                }, style: {
                    marginTop: 12,
                    padding: '6px 12px',
                    fontSize: 12,
                    border: 'none',
                    borderRadius: 4,
                    backgroundColor: '#f5f5f5',
                    color: '#666',
                    cursor: 'pointer',
                    width: '100%',
                }, children: "Clear Filters" }))] }));
};
export default ModelFilter;
//# sourceMappingURL=ModelFilter.js.map