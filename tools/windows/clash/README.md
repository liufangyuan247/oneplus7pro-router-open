# Local Clash OpenAI handoff

The OnePlus router owns OpenAI and CPA Antigravity routing. The local Clash
enhancement removes the old `AI-*` groups and creates only
`AI-OpenAI-Antigravity`, with `DIRECT` first and selected by default. Japanese
nodes remain available in that single group as explicit local overrides.

The enhancement mirrors all 22 entries in the phone's `openai-domains` file as
`DOMAIN-SUFFIX` rules and sends the same suffixes to DNS at `192.168.2.1`.
This includes the exact Cloud Code, OAuth, user-info and updater domains needed
by CPA Antigravity without capturing unrelated Google applications. Claude and
xAI no longer receive local AI-specific groups or rules and fall through to the
subscription's normal policy.

The files beside this README mirror the live Clash Verge enhancement sources.

## Reproducing the no-Clash failure

`run-no-clash-repro.ps1` records the Windows proxy state, router DNS, direct
ChatGPT and CPA connectivity, and two fresh ephemeral Codex probes. The first
probe preserves the new terminal's natural environment; the second clears
proxy environment variables only inside its child process. It does not change
the registry, system proxy, Clash configuration, router, or persistent user
environment.

Close Clash completely, open a new PowerShell window, and run:

```powershell
powershell -ExecutionPolicy Bypass -File "C:\Users\liufangyuan\Desktop\一加7Pro计划\client-clash\run-no-clash-repro.ps1"
```

After the script finishes, reopen Clash and inspect the newest
`client-clash\logs\no-clash-*.log` file. The log survives even if the interactive
Codex session loses its connection while Clash is off.
