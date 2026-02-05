[byte[]]$bom = 0xEF,0xBB,0xBF
$content = [System.IO.File]::ReadAllBytes('D:\project\CodeFlow\issues\2026-02-05_23-13-26-tauri-go-desktop-app.csv')
if($content[0] -ne 0xEF) {
    $newContent = $bom + $content
    [System.IO.File]::WriteAllBytes('D:\project\CodeFlow\issues\2026-02-05_23-13-26-tauri-go-desktop-app.csv', $newContent)
    Write-Host 'BOM added'
} else {
    Write-Host 'BOM already exists'
}
