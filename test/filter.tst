## Test content filtering
read <sample1.fi
=B filter --shell tr e X
=C filter --shell tr e Y
=B filter --regex /This/THIS PATHETIC HEAP/
=C filter --regex /causing/FROBNICATING/
write -
