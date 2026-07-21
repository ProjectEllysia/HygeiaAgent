 <#
Compila hygeia-agent.exe y hygeia-tray.exe, embebe el config.toml del
proyecto (sin agentKey ni bufferPath — ver más abajo) y genera el script de
Inno Setup (.iss) para empaquetarlos en un instalador de Windows de
doble-click. El cliente solo tiene que pegar su agentKey en el tray tras
instalar; serverUrl/intervalSec/collectors ya vienen listos.

Si Inno Setup (ISCC.exe) está instalado, también compila el instalador
final. Si no, deja el .iss listo en dist/ para compilarlo a mano.

Requiere un config.toml real en la raíz del repo (cp config.example.toml
config.toml, rellenar serverUrl) — no se genera aquí a propósito, para que
el desarrollador decida el valor a distribuir.

Sin -Version, toma el contenido de VERSION.txt en la raíz del repo. Para
liberar una versión nueva, basta con actualizar ese fichero.

Uso:
    .\installer\build-installer.ps1                # usa VERSION.txt
    .\installer\build-installer.ps1 -Version 1.0.0  # override puntual
#>
param(
    [string]$Version = (Get-Content (Join-Path (Split-Path -Parent $PSScriptRoot) "VERSION.txt") -Raw).Trim()
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$distDir  = Join-Path $PSScriptRoot "dist"
$issPath  = Join-Path $distDir "hygeia.iss"

New-Item -ItemType Directory -Path $distDir -Force | Out-Null

Push-Location $repoRoot
try {
    Write-Host "==> Compilando hygeia-agent.exe ($Version)"
    go build -ldflags "-X github.com/ProjectEllysia/Ellysia-Hygeia/version.Version=$Version" `
        -o (Join-Path $distDir "hygeia-agent.exe") ./cmd/hygeia-agent
    if ($LASTEXITCODE -ne 0) { throw "fallo compilando hygeia-agent" }

    Write-Host "==> Compilando hygeia-tray.exe ($Version)"
    go build -ldflags "-H=windowsgui -X github.com/ProjectEllysia/Ellysia-Hygeia/version.Version=$Version" `
        -o (Join-Path $distDir "hygeia-tray.exe") ./cmd/hygeia-tray
    if ($LASTEXITCODE -ne 0) { throw "fallo compilando hygeia-tray" }
} finally {
    Pop-Location
}

# Embebe la config del proyecto (serverUrl, intervalSec, collectors) para
# que el cliente solo tenga que pegar su agentKey en el tray, no editar un
# TOML a mano. Se despoja de dos campos antes de empaquetar:
#   - agentKey: es la clave de PRUEBA del desarrollador — distribuirla
#     filtraría esa clave a todos los clientes y los haría colisionar sobre
#     el mismo activo. Cada cliente necesita la suya, vía enrollment (§11.3).
#   - bufferPath: si el config.toml local trae una ruta relativa (para
#     `go run` desde el repo), se resolvería contra el directorio de trabajo
#     del SERVICIO instalado (no el de ProgramData) y rompería el buffer.
#     Omitiéndola, config.Load aplica su default correcto (config.go:75).
$projectConfigPath = Join-Path $repoRoot "config.toml"
if (-not (Test-Path $projectConfigPath)) {
    throw "No se encontró config.toml en la raíz del repo. Créalo (cp config.example.toml config.toml) y rellena al menos serverUrl antes de generar el instalador."
}

Write-Host "==> Embebiendo config.toml del proyecto (sin agentKey ni bufferPath)"
$rawConfig = [System.IO.File]::ReadAllText($projectConfigPath, [System.Text.Encoding]::UTF8)
$configLines = $rawConfig -split "`r`n|`n" |
    Where-Object { $_ -notmatch '^\s*agentKey\s*=' -and $_ -notmatch '^\s*bufferPath\s*=' }
$sanitizedConfig = ($configLines -join "`r`n")

$distConfigPath = Join-Path $distDir "config.toml"
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText($distConfigPath, $sanitizedConfig, $utf8NoBom)

Write-Host "==> Generando $issPath"

$issContent = @"
; Generado por installer\build-installer.ps1 — no editar a mano, el script
; lo sobreescribe en cada build. Cambios permanentes van ahí.
[Setup]
AppId={{86B1FC7E-4F1A-4E1D-9C4B-2C0A1D6E7A11}
AppName=Ellysia Hygeia Agent
AppVersion=$Version
AppPublisher=Ellysia
DefaultDirName={autopf}\Hygeia
DefaultGroupName=Ellysia Hygeia
DisableProgramGroupPage=yes
OutputDir=.
OutputBaseFilename=hygeia-agent-setup-$Version
Compression=lzma
SolidCompression=yes
PrivilegesRequired=admin
; Los binarios Go son amd64 — sin esto, Inno corre en modo 32 bits y
; {autopf} resuelve a "Program Files (x86)" en vez de "Program Files".
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
UninstallDisplayIcon={app}\hygeia-tray.exe
; Icono del propio Setup.exe y de la ventana del wizard (mark sin laurel,
; generado por internal/icon/gen desde resources/hygeia — mismo arte que ya
; usa el tray, ver internal/icon/hygeia-mark.png).
SetupIconFile=$repoRoot\internal\icon\hygeia.ico
; Panel lateral grande del wizard: banner pre-compuesto por
; internal/icon/gen (328x628, mismo ratio de aspecto que el panel de Inno)
; con el laurel completo centrado sobre blanco — la imagen de origen es casi
; cuadrada (1104x960) y, sin este preprocesado, Inno la estira a un óvalo
; deformado (WizardImageStretch=yes, el default) o solo muestra una esquina
; recortada (WizardImageStretch=no); ambos bugs reales, encontrados al
; probar el instalador de verdad. El logo pequeño de arriba a la derecha usa
; el mark sin laurel (se perdería a ese tamaño).
WizardImageFile=$repoRoot\internal\icon\hygeia-wizard.png
WizardSmallImageFile=$repoRoot\resources\hygeia\Hygeia-DarkGreen-BgW.png

[Files]
Source: "hygeia-agent.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "hygeia-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
; onlyifdoesntexist: en una reinstalación/actualización, el cliente ya
; enrolado tiene su agentKey real guardada aquí por el propio servicio
; (config.Save, §5) — nunca se pisa. uninsneveruninstall: desinstalar el
; agente no borra la config del cliente (podría reinstalar más tarde).
Source: "config.toml"; DestDir: "{commonappdata}\Hygeia"; Flags: onlyifdoesntexist uninsneveruninstall

[Icons]
Name: "{group}\Hygeia Tray"; Filename: "{app}\hygeia-tray.exe"
Name: "{group}\Desinstalar Hygeia"; Filename: "{uninstallexe}"

[Run]
; install/start del servicio se mueven a [Code] (CurStepChanged): en una
; actualización necesitan tolerar "ya existe"/"ya en marcha" sin abortar, y
; el propio [Code] para el servicio antes de copiar ficheros (ver más abajo).
; El tray, en cambio, se registra para la SESIÓN del usuario (arranque
; no-admin, §11.1 del README) — runascurrentuser es obligatorio ahí: sin él,
; heredaría el token elevado del instalador y quedaría registrado para la
; cuenta equivocada (SYSTEM/Administrador en vez del usuario real). Esto sí
; puede quedarse en [Run] porque el flag ya resuelve el problema por sí solo.
Filename: "{app}\hygeia-tray.exe"; Parameters: "enable-autostart"; StatusMsg: "Registrando el icono de bandeja..."; Flags: runascurrentuser runhidden waituntilterminated
Filename: "{app}\hygeia-tray.exe"; Description: "Abrir Hygeia ahora"; Flags: postinstall runascurrentuser nowait skipifsilent

[Code]
// El .exe del servicio en marcha bloquea su propio fichero: sobrescribirlo
// en una actualización (el [Files] de arriba) fallaría si no se para antes.
// FileExists evita el intento en una instalación limpia (nada que parar).
// install/start se llaman incondicionalmente en ssPostInstall: en una
// actualización "ya existe"/"ya en marcha" son errores esperados y se
// ignoran a propósito (ResultCode no se comprueba) — es justo lo que pide
// no tener que revocar ni volver a dar de alta la clave: config.toml de
// [Files] no se toca (onlyifdoesntexist) y el servicio simplemente seguirá
// leyendo la misma clave que ya tenía.
procedure CurStepChanged(CurStep: TSetupStep);
var
  ResultCode: Integer;
begin
  if CurStep = ssInstall then
  begin
    if FileExists(ExpandConstant('{app}\hygeia-agent.exe')) then
      Exec(ExpandConstant('{app}\hygeia-agent.exe'), 'stop', '', SW_HIDE,
        ewWaitUntilTerminated, ResultCode);
    // El tray normalmente está siempre abierto (autostart) — si sigue vivo,
    // su .exe está bloqueado y la copia de [Files] fallaría igual que con
    // el servicio. disable-autostart (más abajo) no lo mata, solo evita que
    // vuelva a arrancar solo: por eso hace falta el taskkill aparte.
    Exec(ExpandConstant('{sys}\taskkill.exe'), '/IM hygeia-tray.exe /F', '',
      SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
  if CurStep = ssPostInstall then
  begin
    Exec(ExpandConstant('{app}\hygeia-agent.exe'), 'install', '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode);
    Exec(ExpandConstant('{app}\hygeia-agent.exe'), 'start', '', SW_HIDE,
      ewWaitUntilTerminated, ResultCode);
  end;
end;

[UninstallRun]
; Sin esto, el tray sigue vivo tras "Desinstalar" (disable-autostart de
; abajo solo quita el arranque automático, no cierra el proceso activo) y su
; .exe queda bloqueado cuando Inno intenta borrarlo — la desinstalación
; queda a medias y el propio unins000.exe puede desaparecer antes de
; terminar, dejando una entrada fantasma en "Aplicaciones" que ya no se
; puede volver a desinstalar (bug real encontrado y reproducido en esta sesión).
Filename: "{sys}\taskkill.exe"; Parameters: "/IM hygeia-tray.exe /F"; Flags: runhidden waituntilterminated; RunOnceId: "KillTray"
Filename: "{app}\hygeia-tray.exe"; Parameters: "disable-autostart"; Flags: runascurrentuser runhidden waituntilterminated; RunOnceId: "DisableTrayAutostart"
Filename: "{app}\hygeia-agent.exe"; Parameters: "stop"; Flags: runhidden waituntilterminated; RunOnceId: "StopService"
Filename: "{app}\hygeia-agent.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "UninstallService"
"@

# Inno Setup necesita BOM para detectar UTF-8 (al revés que TOML, que lo
# rechaza — ver la pelea con config.toml de esta misma sesión): sin BOM,
# ISCC reinterpreta los acentos como Windows-1252 y el wizard los corrompe.
$utf8Bom = New-Object System.Text.UTF8Encoding $true
[System.IO.File]::WriteAllText($issPath, $issContent, $utf8Bom)

Write-Host "==> .iss generado en $issPath"

function Find-ISCC {
    $cmd = Get-Command iscc.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    foreach ($candidate in @(
        "$env:ProgramFiles\Inno Setup 6\ISCC.exe",
        "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
        # WinGet, sin admin, instala per-user aquí en vez de Program Files.
        "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe"
    )) {
        if (Test-Path $candidate) { return $candidate }
    }
    return $null
}

$isccPath = Find-ISCC

if (-not $isccPath) {
    $winget = Get-Command winget.exe -ErrorAction SilentlyContinue
    if ($winget) {
        Write-Host "==> Inno Setup no encontrado, instalando con WinGet..."
        & winget.exe install --id JRSoftware.InnoSetup -e `
            --accept-source-agreements --accept-package-agreements
        if ($LASTEXITCODE -eq 0) {
            $isccPath = Find-ISCC
        } else {
            Write-Host "==> WinGet no pudo instalar Inno Setup (código $LASTEXITCODE)."
        }
    } else {
        Write-Host "==> WinGet no está disponible en este equipo para instalar Inno Setup solo."
    }
}

if ($isccPath) {
    Write-Host "==> Compilando instalador con $isccPath"
    & $isccPath $issPath
    if ($LASTEXITCODE -ne 0) { throw "fallo compilando el instalador con ISCC" }
    Write-Host "==> Instalador listo: $distDir\hygeia-agent-setup-$Version.exe"
} else {
    Write-Host ""
    Write-Host "==> Inno Setup (ISCC.exe) no encontrado ni se pudo instalar automáticamente."
    Write-Host "    Instálalo a mano (https://jrsoftware.org/isinfo.php) y compila:"
    Write-Host "    ISCC.exe `"$issPath`""
}
