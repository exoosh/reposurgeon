## Test errors parsing and evaluating disjunctions
set echo

set testmode

read <sample1.fi

3 | 5 list

# This triggers infinite recursion due to changes in version 3.43

3 | 5 | 7 list
