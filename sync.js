const fs = require('fs');
const OSS = require('ali-oss');
const ignore = require('ignore');
const xxHash = require('xxhashjs');
const chalk = require('chalk');
const progress = require('cli-progress');
const Readable = require('stream').Readable;
const cwd = process.cwd();

module.exports = async (opts) => {

    if(!opts.hasOwnProperty('config')) throw 'No config';
    if (!fs.existsSync(opts.config)) throw `config file ${opts.config} not found`;
    const config = JSON.parse(fs.readFileSync(opts.config).toString());

    if(!opts.hasOwnProperty('ignore')) throw 'No ignore';
    const ig = ignore();
    ig.add(opts.config);
    ig.add(opts.ignore);
    if (fs.existsSync(opts.ignore)) ig.add(fs.readFileSync(opts.ignore).toString());

    if(!opts.hasOwnProperty('ignore')) throw 'No force';
    const forceReSyncAll = opts.force;

    const client = new OSS(config);

    await main();

    async function main() {
        console.log(`Sync ${cwd} ---> oss://${config.bucket}`);
        console.log(`Calculating changes...`);

        let ossFiles = forceReSyncAll ? {} : await getOssFiles(ig);
        // console.log(ossFiles);

        let localFiles = await getLocalFiles(ig);
        // console.log(localFiles);

        let createList = [];
        let modifyList = [];
        let removeList = [];

        for (let file in localFiles) {
            if (!localFiles.hasOwnProperty(file)) continue;

            if (ossFiles.hasOwnProperty(file)) {
                if (localFiles[file] !== ossFiles[file]) {
                    modifyList.push(file);
                }
            } else {
                createList.push(file);
            }
        }

        for (let file in ossFiles) {
            if (!ossFiles.hasOwnProperty(file)) continue;

            if (!localFiles.hasOwnProperty(file)) {
                removeList.push(file);
            }
        }

        let totalChanges = createList.length + modifyList.length + removeList.length;

        if (totalChanges === 0) {
            console.log(`Your workspace is already up to date!`);
            process.exit(0);
        }

        console.log('');
        for (let file of createList) console.log(chalk.green(`    Create ${file}`));
        for (let file of modifyList) console.log(chalk.blue(`    Modify ${file}`));
        for (let file of removeList) console.log(chalk.red(`    Remove ${file}`));
        console.log(`\nTotal: ${totalChanges} changes`);

        if(opts.hasOwnProperty('dry') && opts.dry) return;

        let bar = new progress.Bar();
        bar.start(totalChanges, 0);
        let finishedCount = 0;

        for (let file of createList) {
            await client.put(file, file);
            finishedCount++;
            bar.update(finishedCount);
        }

        for (let file of modifyList) {
            await client.put(file, file);
            finishedCount++;
            bar.update(finishedCount);
        }

        for (let file of removeList) {
            await client.delete(file);
            finishedCount++;
            bar.update(finishedCount);
        }
        bar.stop();

        let fileList = JSON.stringify(localFiles, null, 4);
        let s = new Readable();
        s.push(fileList);
        s.push(null);
        console.log(`Commit oss://${config.bucket}/.ossyncfiles`);
        await client.putStream('.ossyncfiles', s);
        console.log(`Your workspace is up to date!`);
    }

    async function getOssFiles(ig) {
        let map = {};
        try {
            let tmpMap = await client.get('.ossyncfiles');
            tmpMap = JSON.parse(tmpMap.res.data.toString());

            for (let file in tmpMap) {
                if (tmpMap.hasOwnProperty(file)) {
                    if (!ig.ignores(file)) {
                        map[file] = tmpMap[file];
                    }
                }
            }
        } catch (e) {
        }
        return map;
    }

    async function getLocalFiles(ig) {
        let map = {};
        let files = readDirRecursively(cwd, ig);
        files = files.map(file => file.substr(cwd.length + 1));

        for (let file of files) {
            map[file] = (await fileHash(file)).toUpperCase();
        }
        return map;
    }

    function readDirRecursively(dir, ig) {
        let files = [];
        fs.readdirSync(dir).forEach(file => {
            let fullPath = dir + '/' + file;
            if (!ig.ignores(fullPath.substr(cwd.length + 1))) {
                if (fs.lstatSync(fullPath).isDirectory()) {
                    files = files.concat(readDirRecursively(fullPath, ig));
                } else {
                    files.push(fullPath);
                }
            }
        });
        return files;
    }

    async function fileHash(file) {
        return new Promise((resolve, reject) => {
            let rs = fs.createReadStream(file);
            let hash = xxHash.h32('ossync');
            rs.on('error', e => {
                reject(e);
            });
            rs.on('data', data => {
                hash.update(data)
            });
            rs.on('end', () => {
                resolve(hash.digest().toString(16));
            });
        });
    }

};

