// Runs the official devcontainers/cli on one workspace and writes what
// official_test.go compares dcx with. record-official.sh runs it inside the
// CLI's container; see there.
'use strict';
const fs = require('fs');
const path = require('path');
const cp = require('child_process');

const ws = process.argv[2];
const configPath = path.join(ws, '.devcontainer', 'devcontainer.json');
const config = JSON.parse(fs.readFileSync(configPath, 'utf8'));

function run(cmd, args, opts = {}) {
	const r = cp.spawnSync(cmd, args, { encoding: 'utf8', maxBuffer: 256 << 20, ...opts });
	if (r.status !== 0) {
		throw new Error(`${cmd} ${args.join(' ')} failed:\n${r.stderr}`);
	}
	return r;
}

// Paths are reported as host paths; the fixtures say <workspace>.
const normalize = (v) => JSON.parse(JSON.stringify(v).split(ws).join('<workspace>'));
const write = (name, v) => fs.writeFileSync(path.join(ws, name), JSON.stringify(normalize(v), null, 2) + '\n');

if (config.features && Object.keys(config.features).length > 0) {
	run('devcontainer', ['upgrade', '--workspace-folder', ws]);
}

function readConfiguration() {
	const r = run('devcontainer', ['read-configuration', '--workspace-folder', ws,
		'--include-features-configuration', '--include-merged-configuration', '--log-level', 'trace']);
	const images = new Set();
	for (const m of r.stderr.matchAll(/Run: docker inspect --type image (\S+)/g)) {
		images.add(m[1]);
	}
	return { rc: JSON.parse(r.stdout), images: [...images].sort() };
}

// The CLI reads an image's metadata from the local image if there is one,
// else from the registry: pull what it inspected and read again, so the
// recorded labels are the ones it used.
let { images } = readConfiguration();
for (const image of images) {
	run('docker', ['pull', '-q', image]);
}
const second = readConfiguration();
const rc = second.rc;
images = second.images;

const sets = rc.featuresConfiguration ? rc.featuresConfiguration.featureSets : [];
const merged = { ...rc.mergedConfiguration };
delete merged.configFilePath; // a host path, and dcx has no such field
delete merged.customizations; // editor settings dcx does not read

write('official.json', {
	cli: process.env.CLI_VERSION,
	featureOrder: sets.map((s) => {
		if (s.features.length !== 1) throw new Error('feature set with several features');
		return { id: s.sourceInformation.userFeatureId, options: s.features[0].value, version: s.features[0].version };
	}),
	workspace: rc.workspace,
	mergedConfiguration: merged,
});

// The registry features as published (the manifest's metadata annotation is
// their devcontainer-feature.json), so dcx can resolve them offline.
const features = {};
for (const s of sets) {
	if (s.sourceInformation.type !== 'oci') continue;
	const annotation = s.sourceInformation.manifest.annotations['dev.containers.metadata'];
	features[s.sourceInformation.manifestDigest] = JSON.parse(annotation);
}
if (Object.keys(features).length > 0) write('features.json', features);

const labels = {};
for (const image of images) {
	const r = run('docker', ['image', 'inspect', '--format', '{{index .Config.Labels "devcontainer.metadata"}}', image]);
	const label = r.stdout.trim();
	labels[image] = label && label !== '<no value>' ? JSON.parse(label) : null;
}
if (images.length > 0) write('image-metadata.json', labels);

if (config.dockerComposeFile) {
	const files = [].concat(config.dockerComposeFile).flatMap((f) => ['-f', f]);
	const r = run('docker', ['compose', '-p', 'fixture', ...files, '--profile', '*', 'config', '--format', 'json'],
		{ cwd: path.dirname(configPath) });
	write('compose-config.json', JSON.parse(r.stdout));
}
