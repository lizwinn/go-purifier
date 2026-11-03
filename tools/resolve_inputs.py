# SPDX-License-Identifier: MIT
"""Pin native Go repositories through the common Nix source adapter."""
import json
import sys
from pathlib import Path
import project_inputs

request = json.load(sys.stdin)
previous = request.get('previous') or []
references = request.get('references')
if references is None:
    references = [entry['reference'] for entry in previous]
inputs = project_inputs.pin(references, previous)
views = project_inputs.native_views(inputs, 'go', request.get('roots'))
json.dump({'inputs': inputs, 'repositories': [str(view['repository']) for view in views]}, sys.stdout)

# Only remove GC roots created by this adapter for no-longer-selected inputs.
root = Path(request['roots'])
if root.exists():
    for path in root.iterdir():
        index = path.name.split('-')[0]
        if path.is_symlink() and index.isdigit() and int(index) >= len(inputs):
            path.unlink()
