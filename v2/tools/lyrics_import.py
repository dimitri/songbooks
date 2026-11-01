#!/usr/bin/env python3
import os
import re
import sys
from pathlib import Path

SECTION_KEYWORDS = r'(intro|outro|verse|verso|prechorus|pre-chorus|chorus|refrain|hook|bridge|interlude|solo|instrumental|breakdown|tag|ending|coda)'

def normalize_sec(s):
    s = s.lower()
    if s in ('prechorus','pre-chorus'): return 'prechorus'
    if s == 'intro': return 'intro'
    if s in ('verse','verso'): return 'verse'
    if s in ('chorus','refrain','hook'): return 'chorus'
    if s == 'bridge': return 'bridge'
    if s in ('interlude','solo','instrumental','breakdown'): return 'solo'
    if s in ('outro','tag','ending','coda'): return 'outro'
    return s

def is_section_line(line):
    m = re.search(r'\b' + SECTION_KEYWORDS + r'\b', line, re.I)
    if m:
        return True, m.group(1)
    stripped = line.strip()
    if stripped and stripped.isupper() and len(stripped) < 25:
        low = stripped.lower()
        if re.match(r'^(intro|verse|prechorus|chorus|bridge|solo|outro|instrumental)$', low):
            return True, low
    return False, None

def detect_sections_in_lyrics(text):
    lines = text.splitlines()
    sections = []
    current_type = 'unknown'
    current_lines = []
    for line in lines:
        is_sec, sec = is_section_line(line)
        if is_sec:
            # flush
            if current_lines or current_type != 'unknown':
                # clean trailing/leading empties a bit
                sections.append((normalize_sec(current_type), current_lines[:]))
            current_type = sec
            current_lines = []
            continue
        current_lines.append(line.rstrip('\r\n'))
    if current_lines:
        sections.append((normalize_sec(current_type), current_lines[:]))
    return sections
def parse_song_file(path):
    with open(path) as f:
        content = f.read()
    map_m = re.search(r'^map:\s*(.+)$', content, re.M)
    map_seq = []
    if map_m:
        mstr = map_m.group(1).replace('·', '|')
        for p in re.split(r'\|', mstr):
            map_seq.append(p.strip())
    return content, map_seq

def get_section_blocks(content):
    blocks = []
    for m in re.finditer(r'^\[([^\]]+)\](?!\s*grid:)', content, re.M):
        name = m.group(1)
        hstart = m.start(); hend = m.end()
        start = hend
        m2 = re.search(r'^\[[^\]]+\]', content[start:], re.M)
        if m2:
            end = start + m2.start()
        else:
            end = len(content)
        blocks.append((name, start, end, hstart, hend))
    return blocks

def block_is_empty(content, start, end):
    for line in content[start:end].splitlines():
        line = line.strip()
        if line and not line.startswith('#') and not line.startswith('grid:'):
            return False
    return True
def align_lyrics_to_blocks(map_seq, blocks, lyric_sections):
    """Assign lyric text to each block in order, trying to match by type.
    lyric_sections is list of (type, lines). Returns list of lyrics for each block (or empty string).
    """
    # Build a queue of lyric sections by type, consuming in order
    # Also handle 'unknown' by trying to infer
    queue = []
    for t, lines in lyric_sections:
        if t == 'unknown' and lines:
            # try to guess from common patterns? skip if looks like metadata
            queue.append(('verse', lines))  # best guess sometimes
        else:
            queue.append((t, lines))
    result = [''] * len(blocks)
    # Try to assign by walking map? Or just by block order and matching types
    # simpler: for each block, look for next matching lyric section
    qi = 0
    for i, (bname, start, end, hstart, hend) in enumerate(blocks):
        btype = normalize_sec(bname.split('(')[0].strip())
        # find next queue item of matching type
        # scan from qi
        for j in range(qi, len(queue)):
            if queue[j][0] == btype or (btype in ('solo','intro','outro') and queue[j][0] == btype):
                # assign
                qtype, qlines = queue.pop(j)
                # join lines
                result[i] = '\n'.join(qlines) + '\n' if qlines else ''
                qi = j  # don't reset to 0
                break
        else:
            # no match found - leave empty
            result[i] = ''
    return result
def format_lyrics_block(lyrics_text):
    # ensure proper spacing
    if not lyrics_text:
        return ''
    # don't add extra blank lines if already formatted
    return lyrics_text

def apply_lyrics_to_song(song_path, lyrics_path=None, dry_run=False):
    with open(song_path) as f:
        content = f.read()
    blocks = get_section_blocks(content)
    if not blocks:
        return False, 'no blocks'
    lyric_sections = []
    if lyrics_path and os.path.exists(lyrics_path):
        with open(lyrics_path) as f:
            txt = f.read()
        lyric_sections = detect_sections_in_lyrics(txt)
    else:
        lyric_sections = []
    assignments = align_lyrics_to_blocks(None, blocks, lyric_sections)
    # better pass map
    _, map_seq = parse_song_file(song_path)
    assignments = align_lyrics_to_blocks(map_seq, blocks, lyric_sections)
    new_content = content
    # apply from bottom to top to preserve indices? easier to reconstruct
    # build new by inserting
    # but easier: go backwards
    offset = 0
    # reconstruct
    parts = []
    pos = 0
    for i, (bname, start, end, hstart, hend) in enumerate(blocks):
        # before block content start, add assignment if block is empty and we have lyrics
        # current content from pos to start
        parts.append(content[pos:start])
        # if block empty, inject lyrics
        block_content = content[start:end]
        is_empty = block_is_empty(content, start, end)
        if is_empty and assignments[i]:
            # add lyrics (strip trailing newlines from block_content area? just add)
            parts.append(assignments[i])
            if not assignments[i].endswith('\n') and block_content and not block_content.startswith('\n'):
                pass  # be careful
            # if block had nothing, we just added; pos moves to end
            pos = end
        else:
            parts.append(block_content)
            pos = end
    parts.append(content[pos:])
    new_content = ''.join(parts)
    if dry_run:
        return True, new_content
    else:
        with open(song_path, 'w') as f:
            f.write(new_content)
        return True, 'written'
def apply_lyrics_to_song(song_path, lyrics_path=None, dry_run=False):
    with open(song_path) as f:
        content = f.read()
    blocks = get_section_blocks(content)
    if not blocks:
        return False, 'no blocks'
    lyric_sections = []
    if lyrics_path and os.path.exists(lyrics_path):
        with open(lyrics_path) as f:
            txt = f.read()
        lyric_sections = detect_sections_in_lyrics(txt)
    _, map_seq = parse_song_file(song_path)
    assignments = align_lyrics_to_blocks(map_seq, blocks, lyric_sections)
    new_content = content
    # reconstruct from right to left
    # easier to do by building result list
    result_parts = []
    prev_end = 0
    for i, (bname, start, end, hstart, hend) in enumerate(blocks):
        result_parts.append(content[prev_end:start])
        block_content = content[start:end]
        is_empty = block_is_empty(content, start, end)
        if is_empty and assignments[i]:
            # insert with proper newline separation
            lyrics = assignments[i]
            # ensure we start on new line
            if lyrics and not lyrics.startswith('\n'):
                lyrics = '\n' + lyrics
            result_parts.append(lyrics)
            prev_end = end
        else:
            result_parts.append(block_content)
            prev_end = end
    result_parts.append(content[prev_end:])
    new_content = ''.join(result_parts)
    if dry_run:
        return True, new_content
    else:
        with open(song_path, 'w') as f:
            f.write(new_content)
        return True, 'written'
