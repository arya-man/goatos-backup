#!/usr/bin/env python3
"""Bind the pinned runner to verified packaged files; never execute a deployment."""
import argparse
import hashlib
import json
import pathlib
import re


def packaged_files(root):
    files = {}
    dockerfile = root / 'deploy/clouddeploy/stg/runner.Dockerfile'
    for line in dockerfile.read_text().splitlines():
        if not line.startswith('COPY ') or line.startswith('COPY --from='):
            continue
        _, source, destination = line.split()
        path = root / source
        if path.is_dir():
            for child in sorted(path.rglob('*')):
                if child.is_file():
                    files[str(child.relative_to(root))] = str(pathlib.PurePosixPath(destination) / child.relative_to(path))
        else:
            files[source] = destination
    if not files:
        raise ValueError('Runner has no packaged source files')
    return files


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def source_hashes(root):
    paths = [*packaged_files(root), 'deploy/clouddeploy/stg/runner.Dockerfile']
    return {p: digest(root / p) for p in sorted(paths)}


def verify_image(root, installed_root):
    for source, destination in packaged_files(root).items():
        if digest(root / source) != digest(installed_root / destination.lstrip('/')):
            raise ValueError('Packaged runner differs from source: ' + source)


def check_receipt(root, receipt):
    if receipt.get('schemaVersion') != 1 or receipt.get('files') != source_hashes(root):
        raise ValueError('Runner source changed; rebuild and verify the runner receipt')
    images = re.findall(r'^    image: (.+)$', (root / 'deploy/clouddeploy/stg/clouddeploy.yaml').read_text(), re.M)
    if len(images) != 2 or any(image != receipt.get('image') for image in images):
        raise ValueError('Render/deploy runner pins must match the verified receipt')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=pathlib.Path, default=pathlib.Path(__file__).resolve().parents[2])
    parser.add_argument('--verify-image', action='store_true')
    parser.add_argument('--image')
    parser.add_argument('--output', type=pathlib.Path)
    args = parser.parse_args()
    if args.verify_image:
        if not args.image or not re.fullmatch(r'asia-south1-docker\.pkg\.dev/goatos-stg/goatos/clouddeploy-stg-runner@sha256:[a-f0-9]{64}', args.image):
            raise ValueError('Immutable staging runner image required')
        if not args.output:
            raise ValueError('Receipt output required')
        verify_image(args.root, pathlib.Path('/'))
        args.output.write_text(json.dumps({'schemaVersion': 1, 'image': args.image, 'files': source_hashes(args.root)}, indent=2) + '\n')
    else:
        check_receipt(args.root, json.loads((args.root / 'deploy/clouddeploy/stg/runner-receipt.json').read_text()))
    print('Runner packaged-source receipt: OK')


if __name__ == '__main__':
    main()
