## Test --empty modifier on mailbox_in
read <min.fi
# Expecting failure on this command
mailbox_in --empty-only <<EOF
------------------------------------------------------------------------------
Author: Ralf Schlatterbeck <rsc@runtux.com>
Author-Date: Thu 01 Jan 1970 00:00:00 +0000

Wearing stompy boots.
------------------------------------------------------------------------------
EOF
