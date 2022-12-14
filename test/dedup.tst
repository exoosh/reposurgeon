## test the dedup function
read <<EOF
blob
mark :1
data 75
This is a toy repo intended as a correctness test for the dedup operation.

blob
mark :2
data 50
This is a file with content in a duplicate blob.


blob
mark :3
data 50
This is a file with content in a duplicate blob.


reset refs/heads/master
commit refs/heads/master
mark :4
author Eric S. Raymond <esr@thyrsus.com> 1575197044 -0500
committer Eric S. Raymond <esr@thyrsus.com> 1575197044 -0500
data 36
This test only requires one commit.
M 100644 :1 README
M 100644 :3 duplicative2
M 100644 :2 duplicative

reset refs/heads/master
from :4

EOF
dedup
write -
