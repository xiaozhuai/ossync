const sade = require('sade');
const prog = sade('ossync');
const sync = require('./sync');
const version = '1.0.1';

prog.version(`v${version}. Copyright 2018 xiaozhuai, Benqumark.`)
    .describe(`ossync v${version}. Copyright 2018 xiaozhuai, Benqumark.`);

prog.command('sync')
    .describe('Sync files.')
    .option('-c, --config', 'Specify config file', '.ossyncconfig')
    .option('-i, --ignore', 'Specify ignore file', '.ossyncignore')
    .option('-f, --force', 'Force re-sync all files', false)
    .action(opts => {
        sync(opts);
    });

prog.command('update')
    .describe('Check new version of ossync.')
    .action(() => {
        console.log('Not implemented yet');
    });

prog.parse(process.argv);


