<# 
train_pipeline.ps1 — Self-play → Train → Loop

Runs iterative cycles of:
  1. Go engine self-play (generates training_data.json)
  2. Python NNUE training (generates nnue_weights.bin)
  3. Copies weights to the server directory

Usage:
  .\scripts\train_pipeline.ps1 -Iterations 5 -Games 500 -Epochs 10
#>

param(
    [int]$Iterations = 5,
    [int]$Games = 500,
    [int]$Epochs = 10,
    [int]$TimePerMove = 100,
    [string]$OutputDir = "services/realtime",
    [double]$TempInit = 0.5,
    [double]$TempFinal = 0.05
)

$ErrorActionPreference = "Stop"
$engineDir = "services/realtime"
$dataFile = Join-Path $OutputDir "training_data.json"
$weightsFile = Join-Path $OutputDir "nnue_weights.bin"
$prevWeights = Join-Path $OutputDir "nnue_weights_prev.bin"

Write-Host "=== 404-Chess Training Pipeline ===" -ForegroundColor Cyan
Write-Host "Iterations: $Iterations | Games/iter: $Games | Epochs: $Epochs" -ForegroundColor Cyan
Write-Host "Temp: ${TempInit} → ${TempFinal} | Time/move: ${TimePerMove}ms" -ForegroundColor Cyan
Write-Host ""

# Ensure Go binary is built.
Write-Host "Building Go engine..." -ForegroundColor Yellow
Push-Location $engineDir
go build -o 404chess-engine.exe ./cmd/404chess-engine/
if (-not $?) { throw "Go build failed" }
Pop-Location

$engineExe = Join-Path $engineDir "404chess-engine.exe"

for ($iter = 1; $iter -le $Iterations; $iter++) {
    Write-Host ""
    Write-Host "=== Iteration $iter / $Iterations ===" -ForegroundColor Green

    # --- Step 1: Self-play ---
    Write-Host "Step 1: Self-play ($Games games)..." -ForegroundColor Yellow
    if ($Iterations -gt 1) {
        $temp = $TempInit - ($iter - 1) * ($TempInit - $TempFinal) / ($Iterations - 1)
    } else {
        $temp = $TempInit
    }
    & $engineExe --selfplay $Games --output $dataFile --temp-init $temp --temp-final $TempFinal --time-per-move $TimePerMove --depth 4
    if (-not $?) { throw "Self-play failed" }

    # --- Step 2: Train ---
    Write-Host "Step 2: Training NNUE ($Epochs epochs)..." -ForegroundColor Yellow
    $trainArgs = @(
        "--data", $dataFile,
        "--epochs", $Epochs,
        "--output", $weightsFile,
        "--batch-size", "256",
        "--lr", "0.001"
    )
    # Load previous weights if available (skip first iteration)
    if ($iter -gt 1 -and (Test-Path $prevWeights)) {
        $trainArgs += "--load", $prevWeights
        Write-Host "  Continuing from $prevWeights" -ForegroundColor Gray
    }

    & python scripts/train_nnue.py @trainArgs
    if (-not $?) { throw "Training failed" }

    # --- Step 3: Save checkpoint ---
    if (Test-Path $weightsFile) {
        Copy-Item $weightsFile $prevWeights -Force
    }

    Write-Host "Iteration $iter complete: weights saved to $weightsFile" -ForegroundColor Green
    Write-Host "------------------------------------------------" -ForegroundColor Gray
}

Write-Host ""
Write-Host "=== Training complete! ===" -ForegroundColor Cyan
Write-Host "Final weights: $weightsFile" -ForegroundColor Cyan
