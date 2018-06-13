#!/bin/bash

sysctl=$(command -v deb-systemd-invoke || echo systemctl)
$sysctl --system daemon-reload >/dev/null || true
if ! $sysctl is-enabled gortcd >/dev/null
then
    $sysctl enable gortcd >/dev/null || true
    $sysctl start gortcd >/dev/null || true
else
    $sysctl restart gortcd >/dev/null || true
fi
