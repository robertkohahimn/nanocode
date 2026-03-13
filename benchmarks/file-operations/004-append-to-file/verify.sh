grep -q 'Entry 1' log.txt && grep -q 'Entry 2' log.txt && grep -q 'Entry 3' log.txt && tail -1 log.txt | grep -q 'Entry 4'
