grok -p Change the word hello to bye in notes.txt, then reply DONE. --output-format json -m grok-4.5 --tools read_file,write,search_replace --permission-mode acceptEdits --cwd <probe-repo>
