// Copyright 2024 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import {Component, Injectable, OnInit, OnDestroy, ViewChild, TemplateRef} from '@angular/core';
import {ActivatedRoute, ActivatedRouteSnapshot, Resolve, Router, RouterStateSnapshot} from '@angular/router';
import {VipAccountList, VipAccount, ConsoleService, UserRole, AddVipAccountRequest, AddVipAccountResponse, VipAccountError, SetVipExpiryAccountRequest} from '../console.service';
import {Observable, Subject} from 'rxjs';
import {UntypedFormBuilder, UntypedFormGroup, Validators, ReactiveFormsModule} from '@angular/forms';
import {AuthenticationService} from '../authentication.service';
import {DeleteConfirmService} from '../shared/delete-confirm.service';
import {takeUntil} from 'rxjs/operators';
import {NgbModal, NgbModule} from '@ng-bootstrap/ng-bootstrap';
import {CommonModule} from '@angular/common';
import {TranslateModule} from '../shared/translate.module';
import {TranslationService} from '../services/translation.service';

@Component({
  templateUrl: './vip-accounts.component.html',
  styleUrls: ['./vip-accounts.component.scss'],
  standalone: true,
  imports: [CommonModule, ReactiveFormsModule, NgbModule, TranslateModule]
})
export class VipAccountsComponent implements OnInit, OnDestroy {
  public error = '';
  public success = '';
  public vipAccountsCount = 0;
  public vipAccounts: Array<VipAccount> = [];
  public nextCursor = '';
  public prevCursor = '';
  public searchForm: UntypedFormGroup;
  public addVipForm: UntypedFormGroup;
  public editVipExpiryForm: UntypedFormGroup;
  public selectedVipForExpiry: VipAccount | null = null;
  public querySubject: Subject<void>;
  public ongoingQuery = false;
  public showAddVipModal = false;

  // 错误modal相关
  currentErrorMessage: string = '';
  @ViewChild('errorModalTemplate') errorModalTemplate!: TemplateRef<any>;

  constructor(
    private readonly route: ActivatedRoute,
    private readonly router: Router,
    private readonly consoleService: ConsoleService,
    private readonly authService: AuthenticationService,
    private readonly formBuilder: UntypedFormBuilder,
    private readonly deleteConfirmService: DeleteConfirmService,
    private readonly modalService: NgbModal,
    private readonly translationService: TranslationService,
  ) {}

  ngOnInit(): void {
    this.querySubject = new Subject<void>();
    this.searchForm = this.formBuilder.group({
      filter: [''],
    });

    this.addVipForm = this.formBuilder.group({
      usernames: ['', Validators.required], // 改为支持多个用户名输入
      expire_time: [''],
    });

    this.editVipExpiryForm = this.formBuilder.group({
      expiry_time: ['', Validators.required],
    });

    const qp = this.route.snapshot.queryParamMap;
    this.f.filter.setValue(qp.get('filter') || '');
    this.nextCursor = qp.get('cursor') || '';

    if (this.nextCursor && this.nextCursor !== '') {
      this.search(1);
    } else if (this.f.filter.value) {
      this.search(0);
    }

    this.route.data.subscribe(
      d => {
        this.vipAccounts.length = 0;
        if (d && d[0]) {
          const accounts = d[0].accounts || [];
          this.vipAccounts.push(...accounts);
          this.vipAccountsCount = d[0].total_count || 0;
          this.nextCursor = d[0].next_cursor || '';
          this.prevCursor = d[0].prev_cursor || '';
        }
      },
      err => {
        this.error = err;
      });
  }

  ngOnDestroy(): void {
    this.querySubject.next();
    this.querySubject.complete();
  }

  search(state: number): void {
    if (this.ongoingQuery) {
      this.querySubject.next();
    }
    this.ongoingQuery = true;

    let cursor = '';
    switch (state) {
      case -1:
        cursor = this.prevCursor;
        break;
      case 0:
        cursor = '';
        break;
      case 1:
        cursor = this.nextCursor;
        break;
    }

    this.consoleService.listVipAccounts('', this.f.filter.value, cursor, 50)
      .pipe(takeUntil(this.querySubject))
      .subscribe(d => {
        this.error = '';

        this.vipAccounts.length = 0;
        this.vipAccounts.push(...(d.accounts || []));
        this.vipAccountsCount = d.total_count || 0;
        this.nextCursor = d.next_cursor || '';
        this.prevCursor = d.prev_cursor || '';

        this.router.navigate([], {
          relativeTo: this.route,
          queryParams: {
            filter: this.f.filter.value,
            cursor
          },
          queryParamsHandling: 'merge',
        });
        this.ongoingQuery = false;
      }, err => {
        this.error = err;
        this.ongoingQuery = false;
      });
  }

  cancelQuery(): void {
    this.querySubject.next();
    this.ongoingQuery = false;
  }

  openAddVipModal(content: any): void {
    this.showAddVipModal = true;
    this.addVipForm.reset();
    this.error = '';
    this.success = '';
    this.modalService.open(content, {ariaLabelledBy: 'modal-basic-title'});
  }

  addVipAccount(): void {
    if (this.addVipForm.invalid) {
      return;
    }

    // 处理多个用户名，按空格分割并过滤空值
    const usernames = this.addVipFormControls.usernames.value
      .trim()
      .split(/\s+/)
      .filter((username: string) => username.trim() !== '');

    if (usernames.length === 0) {
      this.error = this.translationService.translate('Please enter a valid username');
      return;
    }

    // 构建请求
    const request: AddVipAccountRequest = {
      usernames: usernames,
    };

    // 如果设置了过期时间，转换为ISO字符串
    if (this.addVipFormControls.expire_time.value) {
      const expireDate = new Date(this.addVipFormControls.expire_time.value);
      request.expire_time = expireDate.toISOString();
    }

    // 发送批量添加请求
    this.consoleService.addVipAccount('', request).subscribe(
      (response: AddVipAccountResponse) => {
        // 处理成功的账户
        if (response.success_accounts) {
          response.success_accounts.forEach(account => {
            this.vipAccounts.unshift(account);
            this.vipAccountsCount++;
          });
        }

        // 显示结果
        this.showBatchResultFromResponse(response);

        // 关闭模态框并重置表单
        this.modalService.dismissAll();
        this.addVipForm.reset();
      },
      (err) => {
        this.error = err;
        this.success = '';
        console.error('批量添加VIP失败:', err);
      }
    );
  }

  // 从响应中显示批量操作结果
  private showBatchResultFromResponse(response: AddVipAccountResponse): void {
    const successCount = response.success_count || 0;
    const failedCount = response.failed_count || 0;
    const totalCount = response.total_count || 0;

    if (failedCount === 0) {
      this.success = this.translationService.translate('Successfully added') + ` ${successCount} ` + this.translationService.translate('VIP users');
      this.error = '';
    } else if (successCount === 0) {
      this.error = this.translationService.translate('Failed to add') + `, ` + this.translationService.translate('all') + ` ${totalCount} ` + this.translationService.translate('users could not be added');
      this.success = '';

      // 显示详细的失败信息
      if (response.failed_accounts && response.failed_accounts.length > 0) {
        const failedDetails = response.failed_accounts.map(failed =>
          `${failed.username}: ${failed.error_message}`
        ).join('\n');
        this.error = this.translationService.translate('Add VIP failed details') + `\n${failedDetails}`;
      }
    } else {
      this.success = this.translationService.translate('Successfully added') + ` ${successCount} ` + this.translationService.translate('VIP users');
      this.error = this.translationService.translate('Failed to add') + `: ${failedCount} ` + this.translationService.translate('users failed to add');

      // 显示详细的失败信息
      if (response.failed_accounts && response.failed_accounts.length > 0) {
        const failedDetails = response.failed_accounts.map(failed =>
          `${failed.username}: ${failed.error_message}`
        ).join('\n');
        console.warn('添加VIP失败的详细信息:', failedDetails);
      }
    }
  }

  removeVipAccount(event: any, i: number, account: VipAccount): void {
    this.deleteConfirmService.openDeleteConfirmModal(
      () => {
        event.target.disabled = true;
        event.preventDefault();
        this.error = '';
        this.consoleService.removeVipAccount('', account.user_id || '').subscribe(() => {
          this.success = this.translationService.translate('VIP user removed successfully');
          this.error = '';
          // 更新本地数据，将VIP设为失效
          this.vipAccounts[i].is_active = false;
          this.vipAccounts[i].expiry_time = new Date().toISOString();
        }, err => {
          this.error = err;
          this.success = '';
          event.target.disabled = false;
        }, );
      }
    ,undefined, this.translationService.translate('Confirm Delete'), this.translationService.translate('Confirm remove VIP permission?'));
  }

  openEditVipExpiryModal(content: any, account: VipAccount): void {
    this.selectedVipForExpiry = account;
    this.error = '';
    this.success = '';
    this.editVipExpiryForm.reset();

    this.editVipExpiryForm.patchValue({
      expiry_time: this.toDateTimeLocalValue(account.expiry_time),
    });

    this.modalService.open(content, { ariaLabelledBy: 'edit-vip-expiry-title' });
  }

  updateVipExpiryAccount(): void {
    if (this.editVipExpiryForm.invalid || !this.selectedVipForExpiry?.user_id) {
      return;
    }

    const userId = this.selectedVipForExpiry.user_id;
    const expiryLocalValue = this.editVipExpiryFormControls.expiry_time.value as string;
    const expiryIso = new Date(expiryLocalValue).toISOString();

    const request: SetVipExpiryAccountRequest = {
      expiry_time: expiryIso,
    };

    this.consoleService.setVipExpiryAccount('', userId, request).subscribe(
      (vip: VipAccount) => {
        const idx = this.vipAccounts.findIndex(a => a.user_id === userId);
        if (idx >= 0) {
          this.vipAccounts[idx].expiry_time = vip.expiry_time;
          this.vipAccounts[idx].username = vip.username || this.vipAccounts[idx].username;
          this.vipAccounts[idx].is_active = vip.is_active;
        }

        this.success = this.translationService.translate('VIP expiry time updated');
        this.error = '';
        this.modalService.dismissAll();
      },
      (err) => {
        this.error = err;
        this.success = '';
      }
    );
  }

  private toDateTimeLocalValue(dateString: string | undefined): string {
    if (!dateString) return '';
    const d = new Date(dateString);
    if (Number.isNaN(d.getTime())) return '';

    // datetime-local 需要的是“本地时间”格式（没有时区）
    d.setMinutes(d.getMinutes() - d.getTimezoneOffset());
    return d.toISOString().slice(0, 16);
  }

  viewAccount(account: VipAccount): void {
    this.router.navigate(['/accounts', account.user_id]);
  }

  isVipActive(account: VipAccount): boolean {
    if (!account.expiry_time) return false;
    const expireDate = new Date(account.expiry_time);
    return expireDate > new Date();
  }

  formatDate(dateString: string | undefined): string {
    if (!dateString) return '';
    const date = new Date(dateString);
    return date.toLocaleString('zh-CN');
  }

   get f(): any {
    return this.searchForm.controls;
  }

  get addVipFormControls(): any {
    return this.addVipForm.controls;
  }

  get editVipExpiryFormControls(): any {
    return this.editVipExpiryForm.controls;
  }
}

@Injectable({providedIn: 'root'})
export class VipAccountsResolver implements Resolve<VipAccountList> {
  constructor(private readonly consoleService: ConsoleService) {}

  resolve(route: ActivatedRouteSnapshot, state: RouterStateSnapshot): Observable<VipAccountList> {
    const filter = route.queryParamMap.get('filter');
    const cursor = route.queryParamMap.get('cursor');

    return this.consoleService.listVipAccounts('', filter || undefined, cursor || undefined, 50);
  }
}
