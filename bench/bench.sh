#!/bin/bash
set -x

# This script runs reposurgeon multiple times with different readlimit
# values and writes out the run time and peak RSS for each run to a
# file. The file is then sent to gnuplot to make a graph.
#
# Takes four arguments, the dump file to read followed by the minimum,
# step, and maximum values to use for readlimit (just the same
# arguments as seq).
#
# The data will be recorded in a file named after the current git
# revision, as an aid to comparing multiple historical revisions of
# the code. See plot.sh for how we graph the data, and svg.sh for
# turning the data into an svg file you can embed in a web
# page. compare.sh will plot multiple dat files at a time, but it's
# currently specialized for generating oops.svg; you may want to
# customize it to get the graph you want.
#
# New data is simply appended to the end of the data file, so it is
# useful to run this script multiple times with different step
# values. For example, the GCC repository had ~280k revisions. Running
# this first with a step of 50k to see a rough graph in a shorter
# amount of time, followed by runs with smaller step values, such as
# 10k or 1k, is recommended.
#
# BTW, this script uses sudo to instruct the kernel to drop everything
# from the file system cache between runs. For the sake of your own
# sanity, you should configure sudo not to ask you for a password
# every time.

function run {
    datfile="${1}"
    logfile="${2}"
    dump="${3}"
    readlimit="${4}"
    /usr/bin/time -f "${readlimit} %e %M %K" -a -o "${datfile}" \
                  ./reposurgeon "logfile ${logfile}" \
                                "readlimit ${readlimit}" \
                                "read <${dump}" 2>&- 1>&-
}

dump="${1}"
min="${2}"
step="${3}"
max="${4}"
rev="$(git rev-parse HEAD)"
datfile="bench/${dump}_${rev}.dat"
for readlimit in $(seq "${min}" "${step}" "${max}"); do
    sudo sh -c 'echo 3 >/proc/sys/vm/drop_caches'
    run "${datfile}" "bench/${dump}_${rev}_${readlimit}.log" "${dump}" "${readlimit}"
done

#./bench/plot.sh runtime "${datfile}"
