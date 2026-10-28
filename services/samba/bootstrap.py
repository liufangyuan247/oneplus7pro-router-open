"""Create an authenticated, LAN-only Samba share on a rooted Termux router."""
import os
from pathlib import Path
import subprocess

PREFIX = Path('/data/data/com.termux/files/usr')
HOME_DIR = Path('/data/data/com.termux/files/home')
BASE = Path('/data/adb/router-samba')
SHARE = Path('/storage/emulated/0/RouterShare')
CONFIG = BASE / 'smb.conf'

if os.getuid() != 0:
    raise SystemExit('Run bootstrap as root')
if CONFIG.exists():
    raise SystemExit('Refusing to overwrite existing Samba configuration')
os.umask(0o077)
# Termux Samba's Android NSS patch exposes every account as root.
# Keep SMB authentication separate and export only the dedicated directory.
account = 'root'
password_file = Path(os.environ.get('SMB_PASSWORD_FILE', str(HOME_DIR / 'router-data/admin/password')))
password = password_file.read_text().strip()
if not password or '\n' in password:
    raise SystemExit('A nonempty single-line Samba password is required')
if not SHARE.is_dir():
    raise SystemExit('Create the RouterShare directory as the Termux user first')
BASE.mkdir(mode=0o700, exist_ok=True)
for name in ('private', 'state', 'cache', 'lock', 'run'):
    (BASE / name).mkdir(mode=0o700, exist_ok=True)
    if name in ('state', 'cache', 'lock'):
        (BASE / name).chmod(0o755)
(BASE / 'users.map').write_text(f'{account} = router\n')
CONFIG.write_text(f'''[global]
    server role = standalone server
    workgroup = WORKGROUP
    netbios name = OP7ROUTER
    server string = OnePlus Router File Share
    security = user
    map to guest = Never
    username map = {BASE}/users.map
    passdb backend = tdbsam:{BASE}/private/passdb.tdb
    dbwrap_tdb_mutexes:* = no
    dbwrap_tdb_require_mutexes:* = no
    use mmap = no
    private dir = {BASE}/private
    state directory = {BASE}/state
    cache directory = {BASE}/cache
    lock directory = {BASE}/lock
    pid directory = {BASE}/run
    interfaces = 127.0.0.1 eth1
    bind interfaces only = yes
    hosts allow = 127.0.0.1 192.168.2.0/24
    hosts deny = ALL
    smb ports = 445
    disable netbios = yes
    server min protocol = SMB2_10
    server max protocol = SMB3_11
    server signing = mandatory
    server smb encrypt = if_required
    unix extensions = no
    load printers = no
    disable spoolss = yes
    printing = bsd
    printcap name = /dev/null
    log level = 0
    logging = file
    log file = /dev/stdout
    max log size = 0

[Shared]
    comment = Phone shared files
    path = {SHARE}
    browseable = yes
    read only = no
    guest ok = no
    valid users = {account}
    force user = {account}
    force group = {account}
    create mask = 0660
    directory mask = 0770
    follow symlinks = no
    wide links = no
''')
env = dict(os.environ, HOME=str(HOME_DIR), PREFIX=str(PREFIX), PATH=str(PREFIX / 'bin') + ':/system/bin')
with (BASE / 'bootstrap.log').open('wb') as log:
    for args, data in ((['testparm', '-s', str(CONFIG)], None),
                       (['smbpasswd', '-s', '-c', str(CONFIG), '-a', account], (password + '\n' + password + '\n').encode())):
        r = subprocess.run([str(PREFIX / 'bin' / args[0]), *args[1:]], input=data, env=env, stdout=log, stderr=log)
        if r.returncode:
            raise SystemExit('Samba initialization failed; inspect the root-only bootstrap.log')
print('SAMBA_CONFIGURED share=Shared account=router protocol=SMB2/3 signing=required')
