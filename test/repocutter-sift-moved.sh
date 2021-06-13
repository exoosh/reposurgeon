#!/bin/sh
## Test path sifting of moved files and directories from unsifted paths
cd "$(dirname "$0")" || exit
DIR=$(pwd)

trap "rm -fr repocutter-sift-moved-*" EXIT HUP INT QUIT TERM

# create new repo and check it out
svnadmin create repocutter-sift-moved-repo > /dev/null
svn co "file://$DIR/repocutter-sift-moved-repo" repocutter-sift-moved-checkout > /dev/null

# create dir1 and dir2 initially, with only dir1 having files
cd >/dev/null repocutter-sift-moved-checkout || exit 1
mkdir dir1 dir2
cd >/dev/null dir1 || exit 1
echo content1 > file1
echo content2 > file2
cd .. >/dev/null || exit 1
svn add -- * > /dev/null
svn commit -m 'initial commit' > /dev/null

# copy dir1 files to dir2, copy all dir1 to dir3, and edit 
svn copy dir1/file1 dir2 > /dev/null
svn copy dir1 dir3 > /dev/null
svn copy dir3/file2 dir2 > /dev/null
echo line2 >> dir2/file2
svn commit -m 'commit with many changes' > /dev/null

# move all dir1 to dir4 w/ a new file added to it
svn move dir1 dir4 > /dev/null
echo 'line2 in dir4' >> dir4/file2
echo 'new file in dir4' > dir4/anewfile
svn add dir4/anewfile > /dev/null
svn commit -m 'commit of dir4' > /dev/null

cd .. >/dev/null || exit 1
svnadmin dump -q "$DIR/repocutter-sift-moved-repo" | \
    ${REPOCUTTER:-repocutter} -q -repo "file://$DIR/repocutter-sift-moved-repo" sift dir2 dir3 dir4 | \
    ${REPOCUTTER:-repocutter} -q testify